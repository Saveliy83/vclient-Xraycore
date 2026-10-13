package gui

import (
	"context"
	"fmt"
	"net"
	"runtime"
	"strings"
	"time"

	"example.com/vclient/internal/app"
	"example.com/vclient/internal/tunmode"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type connectScreen struct {
	main       *MainWindow
	root       fyne.CanvasObject
	profiles   *widget.Select
	connect    *widget.Button
	status     *widget.Label
	statusIcon *widget.Icon
	progress   *widget.ProgressBarInfinite
	uptime     *widget.Label
	ip         *widget.Label
	fullTunnel *widget.Check
	selected   []string
}

func newConnectScreen(main *MainWindow) *connectScreen {
	return &connectScreen{main: main}
}

func (screen *connectScreen) build() fyne.CanvasObject {
	profiles := screen.main.service.Profiles().List()
	screen.selected = make([]string, len(profiles))
	for index, profile := range profiles {
		screen.selected[index] = profile.Name
	}
	screen.profiles = widget.NewSelect(screen.selected, func(name string) {
		settings := screen.main.service.Settings()
		settings.LastProfile = name
		if err := screen.main.saveSettings(settings); err != nil {
			screen.main.showError(err)
		}
	})
	settings := screen.main.service.Settings()
	if settings.LastProfile != "" {
		screen.profiles.SetSelected(settings.LastProfile)
	} else if len(screen.selected) > 0 {
		screen.profiles.SetSelected(screen.selected[0])
	}
	screen.connect = widget.NewButtonWithIcon("Подключиться", theme.MediaPlayIcon(), screen.toggle)
	screen.connect.Importance = widget.HighImportance
	screen.statusIcon = widget.NewIcon(theme.NewColoredResource(theme.ComputerIcon(), theme.ColorNameWarning))
	screen.status = widget.NewLabel("Отключено")
	screen.progress = widget.NewProgressBarInfinite()
	screen.progress.Hide()
	screen.uptime = widget.NewLabel("Время подключения: 00:00:00")
	screen.ip = widget.NewLabel("")
	screen.fullTunnel = widget.NewCheck("Весь трафик Windows через VPN (TUN)", func(enabled bool) {
		if enabled {
			if err := tunmode.CheckWintunDLL(); err != nil {
				screen.fullTunnel.SetChecked(false)
				screen.main.showError(err)
				return
			}
			if !tunmode.IsAdmin() {
				screen.fullTunnel.SetChecked(false)
				screen.main.showError(fmt.Errorf("enable full-tunnel mode: administrator privileges are required; restart vclient as administrator"))
				return
			}
			dialog.ShowConfirm(
				"Включить полный туннель",
				"Текущий IPv4-трафик будет перенаправлен через VPN. При обрыве VPN клиент попытается восстановить прямой интернет.",
				func(confirm bool) {
					if !confirm {
						screen.fullTunnel.SetChecked(false)
						return
					}
					settings := screen.main.service.Settings()
					settings.FullTunnel = true
					if err := screen.main.saveSettings(settings); err != nil {
						screen.fullTunnel.SetChecked(false)
						screen.main.showError(err)
					}
				},
				screen.main.window,
			)
			return
		}
		settings := screen.main.service.Settings()
		settings.FullTunnel = false
		if err := screen.main.saveSettings(settings); err != nil {
			screen.fullTunnel.SetChecked(true)
			screen.main.showError(err)
		}
	})
	screen.fullTunnel.SetChecked(settings.FullTunnel)
	if runtime.GOOS != "windows" {
		screen.fullTunnel.Disable()
	}
	if settings.FullTunnel {
		if !tunmode.IsAdmin() {
			screen.main.showError(fmt.Errorf("full-tunnel mode requires administrator privileges"))
			screen.fullTunnel.SetChecked(false)
		} else if err := tunmode.CheckWintunDLL(); err != nil {
			screen.main.showError(err)
			screen.fullTunnel.SetChecked(false)
		}
	}
	fullTunnelHelpText := "Требуются права администратора и wintun.dll рядом с vclient-gui.exe. При включённом блокировании IPv6 он временно отключается на всех физических адаптерах; через TUN IPv6 не маршрутизируется."
	if !settings.BlockIPv6 {
		fullTunnelHelpText = "IPv6 остаётся включённым и идёт вне VPN напрямую через физические адаптеры. Через TUN перенаправляется только IPv4."
	}
	fullTunnelHelp := widget.NewLabel(fullTunnelHelpText)
	checkIP := widget.NewButtonWithIcon("Проверить IP", theme.SearchIcon(), screen.checkIP)
	socksCopy := widget.NewButton("Копировать", func() {
		screen.main.app.Clipboard().SetContent(settings.SOCKSAddr)
	})
	httpCopy := widget.NewButton("Копировать", func() {
		screen.main.app.Clipboard().SetContent(settings.HTTPAddr)
	})
	proxyCard := container.NewVBox(
		widget.NewLabelWithStyle("Прокси для приложений", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewBorder(nil, nil, widget.NewLabel("SOCKS5"), socksCopy, widget.NewLabel(settings.SOCKSAddr)),
		container.NewBorder(nil, nil, widget.NewLabel("HTTP"), httpCopy, widget.NewLabel(settings.HTTPAddr)),
	)
	screen.root = container.NewPadded(container.NewVBox(
		widget.NewLabelWithStyle("Подключение", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewForm(widget.NewFormItem("Профиль", screen.profiles)),
		screen.connect,
		container.NewHBox(screen.statusIcon, screen.status, screen.progress),
		screen.uptime,
		container.NewHBox(checkIP, screen.ip),
		screen.fullTunnel,
		fullTunnelHelp,
		widget.NewSeparator(),
		proxyCard,
	))
	screen.updateState(screen.main.service.Snapshot())
	return screen.root
}

func (screen *connectScreen) refreshProfiles() {
	profiles := screen.main.service.Profiles().List()
	selectedName := screen.profiles.Selected
	screen.selected = make([]string, len(profiles))
	for index, profile := range profiles {
		screen.selected[index] = profile.Name
	}
	screen.profiles.Options = screen.selected
	screen.profiles.Refresh()
	if contains(screen.selected, selectedName) {
		screen.profiles.SetSelected(selectedName)
	} else if len(screen.selected) > 0 {
		screen.profiles.SetSelected(screen.selected[0])
	} else {
		screen.profiles.ClearSelected()
	}
}

func (screen *connectScreen) selectedProfileIndex() int {
	for index, name := range screen.selected {
		if name == screen.profiles.Selected {
			return index
		}
	}
	return -1
}

func (screen *connectScreen) toggle() {
	if screen.main.service.State() == app.Connected || screen.main.service.State() == app.Connecting {
		go func() {
			if err := screen.main.service.Disconnect(); err != nil {
				if !strings.Contains(err.Error(), "routes remain after cleanup") {
					fyne.Do(func() { screen.main.showError(err) })
				}
			}
		}()
		return
	}
	index := screen.selectedProfileIndex()
	if index < 0 {
		dialog.ShowError(fmt.Errorf("connect: select a profile first"), screen.main.window)
		return
	}
	go func() {
		if err := screen.main.service.Connect(screen.main.ctx, index); err != nil {
			fyne.Do(func() { screen.main.showError(err) })
		}
	}()
}

func (screen *connectScreen) checkIP() {
	screen.ip.SetText("Проверка...")
	go func() {
		address, err := screen.main.service.CheckExternalIP(context.Background())
		fyne.Do(func() {
			if err != nil {
				screen.ip.SetText("Не удалось проверить IP")
				screen.main.showError(err)
				return
			}
			screen.ip.SetText("Внешний IP: " + net.ParseIP(address).String())
		})
	}()
}

func (screen *connectScreen) updateState(snapshot app.Snapshot) {
	switch snapshot.State {
	case app.Connecting:
		screen.status.SetText("Подключение…")
		screen.statusIcon.SetResource(theme.NewColoredResource(theme.MediaPlayIcon(), theme.ColorNameWarning))
		screen.progress.Show()
		screen.connect.SetText("Подключение…")
		screen.connect.SetIcon(theme.MediaStopIcon())
	case app.Connected:
		screen.status.SetText("Подключено")
		screen.statusIcon.SetResource(theme.NewColoredResource(theme.ConfirmIcon(), theme.ColorNameSuccess))
		screen.progress.Hide()
		screen.connect.SetText("Отключиться")
		screen.connect.SetIcon(theme.MediaStopIcon())
	case app.Failed:
		screen.status.SetText("Ошибка")
		screen.statusIcon.SetResource(theme.NewColoredResource(theme.ErrorIcon(), theme.ColorNameError))
		screen.progress.Hide()
		screen.connect.SetText("Подключиться")
		screen.connect.SetIcon(theme.MediaPlayIcon())
	default:
		screen.status.SetText("Отключено")
		screen.statusIcon.SetResource(theme.NewColoredResource(theme.ComputerIcon(), theme.ColorNameWarning))
		screen.progress.Hide()
		screen.connect.SetText("Подключиться")
		screen.connect.SetIcon(theme.MediaPlayIcon())
	}
	screen.statusIcon.Refresh()
	screen.connect.Refresh()
	if snapshot.State == app.Connected || snapshot.State == app.Connecting {
		screen.fullTunnel.Disable()
	} else if runtime.GOOS == "windows" {
		screen.fullTunnel.Enable()
	}
	screen.updateUptime(time.Now())
}

func (screen *connectScreen) updateUptime(now time.Time) {
	snapshot := screen.main.service.Snapshot()
	if snapshot.State != app.Connected {
		screen.uptime.SetText("Время подключения: 00:00:00")
		return
	}
	screen.uptime.SetText("Время подключения: " + FormatUptime(snapshot.StartedAt, now))
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
