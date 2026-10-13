package gui

import (
	"errors"
	"fmt"
	"runtime"

	"example.com/vclient/internal/app"
	"example.com/vclient/internal/config"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type settingsScreen struct {
	main           *MainWindow
	root           fyne.CanvasObject
	theme          *widget.Select
	socks          *widget.Entry
	http           *widget.Entry
	logLevel       *widget.Select
	systemProxy    *widget.Check
	autoConnect    *widget.Check
	minimizeToTray *widget.Check
	autoStart      *widget.Check
	blockIPv6      *widget.Check
	lastProfile    *widget.Select
}

func newSettingsScreen(main *MainWindow) *settingsScreen {
	return &settingsScreen{main: main}
}

func (screen *settingsScreen) build() fyne.CanvasObject {
	settings := screen.main.service.Settings()
	screen.theme = widget.NewSelect([]string{"system", "light", "dark"}, nil)
	screen.theme.SetSelected(settings.Theme)
	screen.theme.OnChanged = func(value string) {
		current := screen.main.service.Settings()
		current.Theme = value
		if err := screen.persist(current); err != nil {
			screen.main.showError(err)
			return
		}
		screen.main.applyTheme(value)
	}
	screen.socks = entryWithValue(settings.SOCKSAddr)
	screen.socks.SetPlaceHolder("127.0.0.1:1080")
	screen.http = entryWithValue(settings.HTTPAddr)
	screen.http.SetPlaceHolder("127.0.0.1:1081")
	saveAddresses := func() {
		current := screen.main.service.Settings()
		current.SOCKSAddr, current.HTTPAddr = screen.socks.Text, screen.http.Text
		if err := ValidateSettings(current); err != nil {
			screen.main.showError(err)
			screen.refresh()
			return
		}
		if err := screen.persist(current); err != nil {
			screen.main.showError(err)
		}
	}
	screen.socks.OnSubmitted = func(string) { saveAddresses() }
	screen.http.OnSubmitted = func(string) { saveAddresses() }
	screen.logLevel = widget.NewSelect([]string{"debug", "info", "warn", "error"}, nil)
	screen.logLevel.SetSelected(settings.LogLevel)
	screen.logLevel.OnChanged = func(value string) {
		current := screen.main.service.Settings()
		current.LogLevel = value
		if err := screen.persist(current); err != nil {
			screen.main.showError(err)
		}
	}
	screen.systemProxy = widget.NewCheck("Системный прокси Windows", func(enabled bool) {
		current := screen.main.service.Settings()
		if err := screen.main.enableSystemProxy(enabled); err != nil {
			screen.systemProxy.SetChecked(false)
			screen.main.showError(err)
			return
		}
		current.SystemProxy = enabled
		if err := screen.persist(current); err != nil {
			screen.main.showError(err)
		}
	})
	screen.systemProxy.SetChecked(settings.SystemProxy)
	if settings.SystemProxy && screen.main.restoreSystemProxy == nil {
		if err := screen.main.enableSystemProxy(true); err != nil {
			screen.systemProxy.SetChecked(false)
			screen.main.showError(err)
		}
	}
	screen.autoConnect = widget.NewCheck("Автоподключение при запуске", func(enabled bool) {
		current := screen.main.service.Settings()
		current.AutoConnect = enabled
		if err := screen.persist(current); err != nil {
			screen.main.showError(err)
		}
	})
	screen.autoConnect.SetChecked(settings.AutoConnect)
	screen.minimizeToTray = widget.NewCheck("Сворачивать в трей при закрытии", nil)
	screen.minimizeToTray.SetChecked(settings.MinimizeToTray)
	screen.minimizeToTray.OnChanged = func(enabled bool) {
		current := screen.main.service.Settings()
		current.MinimizeToTray = enabled
		if err := screen.persist(current); err != nil {
			setCheckValue(screen.minimizeToTray, !enabled)
			screen.main.showError(err)
		}
	}
	screen.autoStart = widget.NewCheck("Запускать вместе с Windows", nil)
	screen.autoStart.SetChecked(settings.AutoStart)
	screen.autoStart.OnChanged = func(enabled bool) {
		if err := screen.setAutoStart(enabled); err != nil {
			setCheckValue(screen.autoStart, screen.main.service.Settings().AutoStart)
			screen.main.showError(err)
		}
	}
	if runtime.GOOS != "windows" {
		screen.autoStart.Disable()
	}
	screen.blockIPv6 = widget.NewCheck("Временно отключать IPv6 на физических адаптерах во время VPN", func(enabled bool) {
		current := screen.main.service.Settings()
		current.BlockIPv6 = enabled
		if err := screen.persist(current); err != nil {
			setCheckValue(screen.blockIPv6, !enabled)
			screen.main.showError(err)
		}
	})
	screen.blockIPv6.SetChecked(settings.BlockIPv6)
	if runtime.GOOS != "windows" {
		screen.blockIPv6.Disable()
	}
	screen.lastProfile = widget.NewSelect(profileNames(screen.main.service.Profiles().List()), func(value string) {
		current := screen.main.service.Settings()
		current.LastProfile = value
		if err := screen.persist(current); err != nil {
			screen.main.showError(err)
		}
	})
	if settings.LastProfile != "" {
		screen.lastProfile.SetSelected(settings.LastProfile)
	}
	reset := widget.NewButtonWithIcon("Сбросить настройки", theme.ViewRefreshIcon(), func() {
		dialog.ShowConfirm("Сбросить настройки", "Восстановить настройки по умолчанию?", func(confirm bool) {
			if !confirm {
				return
			}
			current := screen.main.service.Settings()
			if current.SystemProxy {
				if err := screen.main.enableSystemProxy(false); err != nil {
					screen.main.showError(err)
					return
				}
			}
			if current.AutoStart && runtime.GOOS == "windows" {
				if err := app.SetAutoStartEnabled(false); err != nil {
					screen.main.showError(err)
					return
				}
			}
			defaults := app.DefaultSettings()
			if err := screen.persist(defaults); err != nil {
				if current.SystemProxy {
					if restoreErr := screen.main.enableSystemProxy(true); restoreErr != nil {
						err = errors.Join(err, fmt.Errorf("restore system proxy: %w", restoreErr))
					}
				}
				if current.AutoStart && runtime.GOOS == "windows" {
					if restoreErr := app.SetAutoStartEnabled(true); restoreErr != nil {
						err = errors.Join(err, fmt.Errorf("restore auto-start setting: %w", restoreErr))
					}
				}
				screen.main.showError(err)
				return
			}
			screen.main.applyTheme(defaults.Theme)
			screen.refresh()
		}, screen.main.window)
	})
	save := widget.NewButtonWithIcon("Сохранить адреса", theme.DocumentSaveIcon(), saveAddresses)
	form := widget.NewForm(
		widget.NewFormItem("Тема", screen.theme),
		widget.NewFormItem("Адрес SOCKS5", screen.socks),
		widget.NewFormItem("Адрес HTTP", screen.http),
		widget.NewFormItem("Уровень логирования", screen.logLevel),
		widget.NewFormItem("", screen.systemProxy),
		widget.NewFormItem("", screen.autoConnect),
		widget.NewFormItem("", screen.minimizeToTray),
		widget.NewFormItem("", screen.autoStart),
		widget.NewFormItem("", screen.blockIPv6),
		widget.NewFormItem("Последний профиль", screen.lastProfile),
	)
	screen.root = container.NewPadded(container.NewVBox(
		widget.NewLabelWithStyle("Настройки", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		form, container.NewHBox(save, reset),
	))
	return screen.root
}

func (screen *settingsScreen) setAutoStart(enabled bool) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("configure auto-start: %w", errors.New("only supported on Windows"))
	}
	current := screen.main.service.Settings()
	if err := app.SetAutoStartEnabled(enabled); err != nil {
		return err
	}
	current.AutoStart = enabled
	if err := screen.persist(current); err != nil {
		if restoreErr := app.SetAutoStartEnabled(!enabled); restoreErr != nil {
			return errors.Join(err, fmt.Errorf("restore auto-start registration: %w", restoreErr))
		}
		return err
	}
	return nil
}

func (screen *settingsScreen) persist(settings app.Settings) error {
	if err := screen.main.saveSettings(settings); err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	return nil
}

func (screen *settingsScreen) refresh() {
	if screen.root == nil {
		return
	}
	settings := screen.main.service.Settings()
	screen.theme.SetSelected(settings.Theme)
	screen.socks.SetText(settings.SOCKSAddr)
	screen.http.SetText(settings.HTTPAddr)
	screen.logLevel.SetSelected(settings.LogLevel)
	screen.systemProxy.SetChecked(settings.SystemProxy)
	screen.autoConnect.SetChecked(settings.AutoConnect)
	setCheckValue(screen.minimizeToTray, settings.MinimizeToTray)
	setCheckValue(screen.autoStart, settings.AutoStart)
	setCheckValue(screen.blockIPv6, settings.BlockIPv6)
	screen.refreshProfiles()
	screen.lastProfile.SetSelected(settings.LastProfile)
}

func setCheckValue(check *widget.Check, value bool) {
	onChanged := check.OnChanged
	check.OnChanged = nil
	check.SetChecked(value)
	check.OnChanged = onChanged
}

func (screen *settingsScreen) refreshProfiles() {
	if screen.lastProfile == nil {
		return
	}
	screen.lastProfile.Options = profileNames(screen.main.service.Profiles().List())
	screen.lastProfile.Refresh()
}

func profileNames(profiles []config.Profile) []string {
	names := make([]string, len(profiles))
	for index, profile := range profiles {
		names[index] = profile.Name
	}
	return names
}
