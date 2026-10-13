package gui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"example.com/vclient/internal/config"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type profilesScreen struct {
	main          *MainWindow
	root          fyne.CanvasObject
	list          *widget.List
	link          *widget.Entry
	subscription  *widget.Entry
	subProgress   *widget.ProgressBarInfinite
	selectedIndex int
}

func newProfilesScreen(main *MainWindow) *profilesScreen {
	screen := &profilesScreen{main: main, selectedIndex: -1}
	screen.list = widget.NewList(
		func() int { return len(screen.main.service.Profiles().List()) },
		func() fyne.CanvasObject { return newProfileListItem() },
		func(id widget.ListItemID, object fyne.CanvasObject) {
			profiles := screen.main.service.Profiles().List()
			row := object.(*fyne.Container)
			title := row.Objects[0].(*widget.Label)
			details := row.Objects[1].(*widget.Label)
			if id < 0 || id >= len(profiles) {
				title.SetText("")
				details.SetText("")
				return
			}
			profile := profiles[id]
			title.SetText(profile.Name)
			details.SetText(fmt.Sprintf("%s · %s:%d", profile.Network, profile.Address, profile.Port))
		},
	)
	screen.list.OnSelected = func(id widget.ListItemID) { screen.selectedIndex = int(id) }
	return screen
}

func newProfileListItem() fyne.CanvasObject {
	return container.NewVBox(widget.NewLabel("Profile"), widget.NewLabel(""))
}

func (screen *profilesScreen) build() fyne.CanvasObject {
	screen.link = widget.NewEntry()
	screen.link.SetPlaceHolder("vless://…")
	addLink := widget.NewButtonWithIcon("Добавить", theme.ContentAddIcon(), screen.addLink)
	linkRow := container.NewBorder(nil, nil, nil, addLink, screen.link)

	screen.subscription = widget.NewEntry()
	screen.subscription.SetPlaceHolder("https://…")
	screen.subProgress = widget.NewProgressBarInfinite()
	screen.subProgress.Hide()
	loadSubscription := widget.NewButtonWithIcon("Загрузить подписку", theme.DownloadIcon(), screen.loadSubscription)
	subRow := container.NewVBox(container.NewBorder(nil, nil, nil, loadSubscription, screen.subscription), screen.subProgress)

	actions := container.NewHBox(
		widget.NewButtonWithIcon("Изменить", theme.DocumentCreateIcon(), screen.editSelected),
		widget.NewButtonWithIcon("Удалить", theme.DeleteIcon(), screen.deleteSelected),
		widget.NewButtonWithIcon("Сделать активным", theme.MediaPlayIcon(), screen.activateSelected),
		widget.NewButtonWithIcon("Импорт JSON", theme.FolderOpenIcon(), screen.importJSON),
	)
	importCard := widget.NewCard("Импорт профилей", "", container.NewVBox(
		widget.NewLabel("Ссылка vless://"), linkRow,
		widget.NewLabel("URL подписки"), subRow,
		widget.NewButtonWithIcon("Выбрать JSON-профиль", theme.DocumentIcon(), screen.importJSON),
	))
	screen.root = container.NewPadded(container.NewBorder(
		widget.NewLabelWithStyle("Профили", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewVBox(actions, importCard),
		nil, nil,
		screen.list,
	))
	screen.refresh()
	return screen.root
}

func (screen *profilesScreen) refresh() {
	if screen.list != nil {
		screen.list.Refresh()
	}
	if screen.main.connect != nil {
		screen.main.connect.refreshProfiles()
	}
	if screen.main.settings != nil {
		screen.main.settings.refreshProfiles()
	}
}

func (screen *profilesScreen) addLink() {
	profile, warnings, err := screen.main.service.Profiles().AddFromLink(screen.link.Text)
	if err != nil {
		screen.main.showError(err)
		return
	}
	screen.link.SetText("")
	screen.refresh()
	if len(warnings) > 0 {
		dialog.ShowInformation("Предупреждения профиля "+profile.Name, fmt.Sprint(warnings), screen.main.window)
	}
}

func (screen *profilesScreen) loadSubscription() {
	url := screen.subscription.Text
	if url == "" {
		screen.main.showError(fmt.Errorf("subscription URL: %w", errors.New("URL must not be empty")))
		return
	}
	screen.subProgress.Show()
	go func() {
		added, err := screen.main.service.Profiles().AddFromSubscription(screen.main.ctx, url)
		fyne.Do(func() {
			screen.subProgress.Hide()
			if err != nil {
				screen.main.showError(err)
				return
			}
			screen.subscription.SetText("")
			screen.refresh()
			dialog.ShowInformation("Подписка", fmt.Sprintf("Добавлено профилей: %d", added), screen.main.window)
		})
	}()
}

func (screen *profilesScreen) selectedProfile() (config.Profile, bool) {
	return screen.main.service.Profiles().Get(screen.selectedIndex)
}

func (screen *profilesScreen) deleteSelected() {
	profile, ok := screen.selectedProfile()
	if !ok {
		screen.main.showError(fmt.Errorf("delete profile: %w", errors.New("select a profile first")))
		return
	}
	dialog.ShowConfirm("Удалить профиль", "Удалить профиль «"+profile.Name+"»?", func(confirm bool) {
		if !confirm {
			return
		}
		if err := screen.main.service.Profiles().Delete(screen.selectedIndex); err != nil {
			screen.main.showError(err)
			return
		}
		screen.selectedIndex = -1
		screen.refresh()
	}, screen.main.window)
}

func (screen *profilesScreen) activateSelected() {
	if _, ok := screen.selectedProfile(); !ok {
		screen.main.showError(fmt.Errorf("activate profile: %w", errors.New("select a profile first")))
		return
	}
	index := screen.selectedIndex
	go func() {
		if err := screen.main.service.Connect(screen.main.ctx, index); err != nil {
			fyne.Do(func() { screen.main.showError(err) })
		}
	}()
}

func (screen *profilesScreen) importJSON() {
	fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			screen.main.showError(err)
			return
		}
		if reader == nil {
			return
		}
		body, err := io.ReadAll(io.LimitReader(reader, 1<<20))
		closeErr := reader.Close()
		if err != nil || closeErr != nil {
			if err != nil {
				err = fmt.Errorf("read profile file: %w", err)
			}
			if closeErr != nil {
				err = errors.Join(err, fmt.Errorf("close profile file: %w", closeErr))
			}
			screen.main.showError(err)
			return
		}
		var profile config.Profile
		if err := json.Unmarshal(body, &profile); err != nil {
			screen.main.showError(fmt.Errorf("decode profile file: %w", err))
			return
		}
		if err := screen.main.service.Profiles().Add(profile); err != nil {
			screen.main.showError(err)
			return
		}
		screen.refresh()
	}, screen.main.window)
	fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{".json"}))
	fileDialog.Show()
}

func (screen *profilesScreen) editSelected() {
	profile, ok := screen.selectedProfile()
	if !ok {
		screen.main.showError(fmt.Errorf("edit profile: %w", errors.New("select a profile first")))
		return
	}
	screen.showProfileForm(profile, screen.selectedIndex)
}

func (screen *profilesScreen) showProfileForm(profile config.Profile, index int) {
	name := entryWithValue(profile.Name)
	address := entryWithValue(profile.Address)
	port := entryWithValue(strconv.Itoa(profile.Port))
	uuid := entryWithValue(profile.UUID)
	flow := entryWithValue(profile.Flow)
	network := widget.NewSelect([]string{"tcp", "ws", "grpc", "xhttp", "httpupgrade", "kcp", "quic"}, nil)
	network.SetSelected(profile.Network)
	security := widget.NewSelect([]string{"none", "tls", "reality"}, nil)
	security.SetSelected(profile.Security)
	tlsServerName := entryWithValue(profile.TLS.ServerName)
	tlsALPN := entryWithValue(strings.Join(profile.TLS.ALPN, ", "))
	tlsFingerprint := entryWithValue(profile.TLS.Fingerprint)
	tlsAllowInsecure := widget.NewCheck("Разрешить недействительный сертификат", nil)
	tlsAllowInsecure.SetChecked(profile.TLS.AllowInsecure)
	realityServerName := entryWithValue(profile.Reality.ServerName)
	realityFingerprint := entryWithValue(profile.Reality.Fingerprint)
	pbk := entryWithValue(profile.Reality.PublicKey)
	shortID := entryWithValue(profile.Reality.ShortID)
	spiderX := entryWithValue(profile.Reality.SpiderX)
	wsPath := entryWithValue(profile.WS.Path)
	wsHost := entryWithValue(profile.WS.Host)
	grpcService := entryWithValue(profile.GRPC.ServiceName)
	grpcMultiMode := widget.NewCheck("gRPC multiMode", nil)
	grpcMultiMode.SetChecked(profile.GRPC.MultiMode)
	xhttpPath := entryWithValue(profile.XHTTP.Path)
	xhttpHost := entryWithValue(profile.XHTTP.Host)
	xhttpMode := widget.NewSelect([]string{"auto", "packet-up", "stream-up", "stream-one"}, nil)
	xhttpMode.SetSelected(profile.XHTTP.Mode)
	xhttpHeadersJSON, err := json.Marshal(profile.XHTTP.Headers)
	if err != nil {
		screen.main.showError(fmt.Errorf("encode XHTTP headers: %w", err))
		return
	}
	xhttpHeaders := entryWithValue(string(xhttpHeadersJSON))
	upgradePath := entryWithValue(profile.HTTPUpgrade.Path)
	upgradeHost := entryWithValue(profile.HTTPUpgrade.Host)
	kcpHeaderType := entryWithValue(profile.KCP.HeaderType)
	kcpSeed := entryWithValue(profile.KCP.Seed)
	kcpMTU := entryWithValue(strconv.FormatUint(uint64(profile.KCP.MTU), 10))
	kcpTTI := entryWithValue(strconv.FormatUint(uint64(profile.KCP.TTI), 10))

	items := []*widget.FormItem{
		widget.NewFormItem("Имя", name), widget.NewFormItem("Адрес сервера", address),
		widget.NewFormItem("Порт", port), widget.NewFormItem("UUID", uuid),
		widget.NewFormItem("Транспорт", network), widget.NewFormItem("Безопасность", security),
		widget.NewFormItem("Flow", flow), widget.NewFormItem("TLS SNI", tlsServerName),
		widget.NewFormItem("TLS ALPN (через запятую)", tlsALPN),
		widget.NewFormItem("TLS fingerprint", tlsFingerprint),
		widget.NewFormItem("", tlsAllowInsecure),
		widget.NewFormItem("REALITY SNI", realityServerName),
		widget.NewFormItem("REALITY fingerprint", realityFingerprint),
		widget.NewFormItem("REALITY public key", pbk), widget.NewFormItem("REALITY short ID", shortID),
		widget.NewFormItem("REALITY spiderX", spiderX),
		widget.NewFormItem("WS path", wsPath), widget.NewFormItem("WS host", wsHost),
		widget.NewFormItem("gRPC service", grpcService), widget.NewFormItem("", grpcMultiMode),
		widget.NewFormItem("XHTTP path", xhttpPath), widget.NewFormItem("XHTTP host", xhttpHost),
		widget.NewFormItem("XHTTP mode", xhttpMode), widget.NewFormItem("XHTTP headers (JSON)", xhttpHeaders),
		widget.NewFormItem("HTTPUpgrade path", upgradePath), widget.NewFormItem("HTTPUpgrade host", upgradeHost),
		widget.NewFormItem("KCP header type", kcpHeaderType), widget.NewFormItem("KCP seed", kcpSeed),
		widget.NewFormItem("KCP MTU", kcpMTU), widget.NewFormItem("KCP TTI", kcpTTI),
	}
	form := dialog.NewForm("Профиль", "Сохранить", "Отмена", items, func(confirm bool) {
		if !confirm {
			return
		}
		parsedPort, err := strconv.Atoi(port.Text)
		if err != nil {
			screen.main.showError(fmt.Errorf("profile port: %w", err))
			return
		}
		updated := profile
		updated.Name, updated.Address, updated.Port, updated.UUID = name.Text, address.Text, parsedPort, uuid.Text
		updated.Flow, updated.Network, updated.Security = flow.Text, network.Selected, security.Selected
		updated.TLS.ServerName, updated.TLS.Fingerprint = tlsServerName.Text, tlsFingerprint.Text
		updated.TLS.ALPN = splitCommaValues(tlsALPN.Text)
		updated.TLS.AllowInsecure = tlsAllowInsecure.Checked
		updated.Reality.ServerName, updated.Reality.Fingerprint = realityServerName.Text, realityFingerprint.Text
		updated.Reality.PublicKey, updated.Reality.ShortID, updated.Reality.SpiderX = pbk.Text, shortID.Text, spiderX.Text
		updated.WS.Path, updated.WS.Host = wsPath.Text, wsHost.Text
		updated.GRPC.ServiceName, updated.GRPC.MultiMode = grpcService.Text, grpcMultiMode.Checked
		updated.XHTTP.Path, updated.XHTTP.Host, updated.XHTTP.Mode = xhttpPath.Text, xhttpHost.Text, xhttpMode.Selected
		if err := json.Unmarshal([]byte(xhttpHeaders.Text), &updated.XHTTP.Headers); err != nil {
			screen.main.showError(fmt.Errorf("decode XHTTP headers: %w", err))
			return
		}
		updated.HTTPUpgrade.Path, updated.HTTPUpgrade.Host = upgradePath.Text, upgradeHost.Text
		updated.KCP.HeaderType, updated.KCP.Seed = kcpHeaderType.Text, kcpSeed.Text
		updated.KCP.MTU, err = parseUint32Field("KCP MTU", kcpMTU.Text)
		if err != nil {
			screen.main.showError(err)
			return
		}
		updated.KCP.TTI, err = parseUint32Field("KCP TTI", kcpTTI.Text)
		if err != nil {
			screen.main.showError(err)
			return
		}
		if err := ValidateProfileForm(updated); err != nil {
			screen.main.showError(err)
			return
		}
		if err := screen.main.service.Profiles().Update(index, updated); err != nil {
			screen.main.showError(err)
			return
		}
		screen.refresh()
	}, screen.main.window)
	form.Resize(fyne.NewSize(600, 620))
	form.Show()
}

func entryWithValue(value string) *widget.Entry {
	entry := widget.NewEntry()
	entry.SetText(value)
	return entry
}

func splitCommaValues(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}

func parseUint32Field(name, value string) (uint32, error) {
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return uint32(parsed), nil
}
