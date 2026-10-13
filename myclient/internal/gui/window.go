package gui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"example.com/vclient/internal/app"
	"example.com/vclient/internal/systemproxy"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

var sections = []string{"Подключение", "Профили", "Логи", "Настройки", "О программе"}

type MainWindow struct {
	app          fyne.App
	window       fyne.Window
	service      *app.Service
	settingsPath string
	logCloser    io.Closer
	dataDir      string
	version      string
	ctx          context.Context
	cancel       context.CancelFunc
	shutdownOnce sync.Once

	navigation *widget.List
	content    *fyne.Container
	connect    *connectScreen
	profiles   *profilesScreen
	logs       *logsScreen
	settings   *settingsScreen
	about      fyne.CanvasObject

	restoreSystemProxy func() error
	trayApp            desktop.App
	trayMenu           *fyne.Menu
	trayConnectItem    *fyne.MenuItem
	lastState          app.ConnState
}

func NewWindow(application fyne.App, service *app.Service, settingsPath, version string, logCloser io.Closer) *MainWindow {
	ctx, cancel := context.WithCancel(context.Background())
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = "."
	}
	dataDir := filepath.Join(configDir, "vclient")
	if settingsPath == "" {
		settingsPath = filepath.Join(dataDir, "settings.json")
	}
	main := &MainWindow{
		app: application, service: service, settingsPath: settingsPath,
		logCloser: logCloser, dataDir: dataDir, version: version,
		ctx: ctx, cancel: cancel,
		lastState: service.State(),
	}
	main.window = application.NewWindow("vclient")
	main.window.SetIcon(IconResource())
	main.window.Resize(fyne.NewSize(980, 640))
	main.window.SetContent(container.NewGridWrap(fyne.NewSize(800, 560), main.build()))
	main.window.SetMainMenu(main.buildMenu())
	main.window.Canvas().AddShortcut(&desktop.CustomShortcut{
		KeyName: fyne.KeyK, Modifier: fyne.KeyModifierControl,
	}, func(fyne.Shortcut) { main.toggleConnection() })
	main.window.SetCloseIntercept(main.closeIntercept)
	main.setupTray()
	service.Subscribe(func(snapshot app.Snapshot) {
		fyne.Do(func() {
			previousState := main.lastState
			main.lastState = snapshot.State
			main.refreshState(snapshot)
			main.refreshTray(snapshot.State)
			if snapshot.State == app.Failed && previousState == app.Connected &&
				strings.Contains(snapshot.LastError, "routes remain after cleanup") {
				main.showError(errors.New(snapshot.LastError))
			}
		})
	})
	service.SubscribeLogs(func(app.LogEntry) {
		fyne.Do(func() {
			if main.logs != nil {
				main.logs.refresh()
			}
		})
	})
	go main.updateUptime()
	return main
}

func (main *MainWindow) setupTray() {
	trayApp, ok := main.app.(desktop.App)
	if !ok {
		return
	}
	main.trayApp = trayApp
	main.trayConnectItem = fyne.NewMenuItem("Подключиться", func() {
		fyne.Do(main.toggleConnection)
	})
	showItem := fyne.NewMenuItem("Показать окно", func() {
		fyne.Do(main.window.Show)
	})
	quitItem := fyne.NewMenuItem("Выход", func() {
		go main.exitApplication()
	})
	main.trayMenu = fyne.NewMenu("vclient", showItem, main.trayConnectItem,
		fyne.NewMenuItemSeparator(), quitItem)
	trayApp.SetSystemTrayIcon(IconResource())
	trayApp.SetSystemTrayMenu(main.trayMenu)
	main.refreshTray(main.service.State())
}

func (main *MainWindow) refreshTray(state app.ConnState) {
	if main.trayMenu == nil || main.trayConnectItem == nil {
		return
	}
	if state == app.Connected || state == app.Connecting {
		main.trayConnectItem.Label = "Отключиться"
	} else {
		main.trayConnectItem.Label = "Подключиться"
	}
	main.trayApp.SetSystemTrayMenu(main.trayMenu)
}

func (main *MainWindow) Window() fyne.Window {
	return main.window
}

func (main *MainWindow) Sections() []string {
	return append([]string(nil), sections...)
}

func (main *MainWindow) Show() {
	main.window.Show()
}

func (main *MainWindow) ConnectLastProfile(name string) {
	profiles := main.service.Profiles().List()
	for index, profile := range profiles {
		if profile.Name == name {
			go func() {
				if err := main.service.Connect(main.ctx, index); err != nil {
					fyne.Do(func() { main.showError(err) })
				}
			}()
			return
		}
	}
	main.service.Logger().Warn("last profile was not found")
}

func (main *MainWindow) build() fyne.CanvasObject {
	main.navigation = widget.NewList(
		func() int { return len(sections) },
		func() fyne.CanvasObject {
			return widget.NewButtonWithIcon("", theme.ComputerIcon(), nil)
		},
		func(id widget.ListItemID, object fyne.CanvasObject) {
			button := object.(*widget.Button)
			button.SetText(sections[id])
			button.SetIcon(sectionIcon(id))
			button.OnTapped = func() { main.showSection(id) }
		},
	)
	main.navigation.Select(0)
	main.content = container.NewStack()
	main.showSection(0)
	sidebar := container.NewBorder(
		widget.NewLabelWithStyle("vclient", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		nil, nil, nil,
		container.NewPadded(main.navigation),
	)
	return container.NewBorder(nil, nil, container.NewGridWrap(fyne.NewSize(200, 600), sidebar), nil, main.content)
}

func (main *MainWindow) showSection(index int) {
	if index < 0 || index >= len(sections) {
		return
	}
	if main.navigation != nil {
		main.navigation.Select(index)
	}
	var screen fyne.CanvasObject
	switch index {
	case 0:
		if main.connect == nil {
			main.connect = newConnectScreen(main)
		}
		screen = main.connect.build()
	case 1:
		if main.profiles == nil {
			main.profiles = newProfilesScreen(main)
		}
		screen = main.profiles.build()
	case 2:
		if main.logs == nil {
			main.logs = newLogsScreen(main)
		}
		screen = main.logs.build()
	case 3:
		if main.settings == nil {
			main.settings = newSettingsScreen(main)
		}
		screen = main.settings.build()
	case 4:
		if main.about == nil {
			main.about = main.buildAbout()
		}
		screen = main.about
	}
	main.content.Objects = []fyne.CanvasObject{screen}
	main.content.Refresh()
}

func (main *MainWindow) showError(err error) {
	if err != nil {
		dialog.ShowError(err, main.window)
	}
}

func (main *MainWindow) buildAbout() fyne.CanvasObject {
	goVersion := runtime.Version()
	path := main.dataDir
	open := widget.NewButtonWithIcon("Открыть каталог данных", theme.FolderOpenIcon(), func() {
		if err := openDirectory(main.dataDir); err != nil {
			dialog.ShowError(err, main.window)
		}
	})
	return container.NewPadded(container.NewVBox(
		widget.NewIcon(theme.InfoIcon()),
		widget.NewLabelWithStyle("vclient", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Версия: "+main.version),
		widget.NewLabel("Go: "+goVersion),
		widget.NewLabel("Каталог данных: "+path),
		open,
	))
}

func (main *MainWindow) updateUptime() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-main.ctx.Done():
			return
		case now := <-ticker.C:
			fyne.Do(func() {
				if main.connect != nil {
					main.connect.updateUptime(now)
				}
			})
		}
	}
}

func (main *MainWindow) refreshState(snapshot app.Snapshot) {
	if main.connect != nil {
		main.connect.updateState(snapshot)
	}
	if main.profiles != nil {
		main.profiles.refresh()
	}
}

func (main *MainWindow) toggleConnection() {
	if main.service.State() == app.Connected || main.service.State() == app.Connecting {
		go func() {
			if err := main.service.Disconnect(); err != nil {
				if !strings.Contains(err.Error(), "routes remain after cleanup") {
					fyne.Do(func() { dialog.ShowError(err, main.window) })
				}
			}
		}()
		return
	}
	if main.connect == nil {
		main.showSection(0)
	}
	index := main.connect.selectedProfileIndex()
	if index < 0 {
		dialog.ShowError(fmt.Errorf("connect: %w", errors.New("select a profile first")), main.window)
		return
	}
	go func() {
		if err := main.service.Connect(main.ctx, index); err != nil {
			fyne.Do(func() { dialog.ShowError(err, main.window) })
		}
	}()
}

func (main *MainWindow) closeIntercept() {
	settings := main.service.Settings()
	if ActionOnClose(settings, main.trayApp != nil) == HideWindowToTray {
		main.window.Hide()
		return
	}
	main.exitApplication()
}

func (main *MainWindow) exitApplication() {
	main.shutdownOnce.Do(func() {
		main.cancel()
		var shutdownErrors []error
		if main.restoreSystemProxy != nil {
			if err := main.restoreSystemProxy(); err != nil {
				shutdownErrors = append(shutdownErrors, fmt.Errorf("restore system proxy: %w", err))
			}
		}
		if err := main.service.Shutdown(); err != nil {
			shutdownErrors = append(shutdownErrors, err)
		}
		if main.logCloser != nil {
			if err := main.logCloser.Close(); err != nil {
				shutdownErrors = append(shutdownErrors, fmt.Errorf("close application log: %w", err))
			}
		}
		if len(shutdownErrors) > 0 {
			main.service.Logger().Error("application shutdown failed", "error", errors.Join(shutdownErrors...))
		}
		main.window.SetCloseIntercept(nil)
		main.app.Quit()
	})
}

func (main *MainWindow) saveSettings(settings app.Settings) error {
	if err := main.service.UpdateSettings(settings); err != nil {
		return err
	}
	if err := app.SaveSettingsTo(main.settingsPath, settings); err != nil {
		return err
	}
	return nil
}

func (main *MainWindow) enableSystemProxy(enabled bool) error {
	settings := main.service.Settings()
	if !systemproxy.Supported() {
		return fmt.Errorf("system proxy: %w", errors.New("not supported on this operating system"))
	}
	if enabled {
		if main.restoreSystemProxy != nil {
			return nil
		}
		restore, err := systemproxy.Apply(settings.SOCKSAddr, settings.HTTPAddr)
		if err != nil {
			return err
		}
		main.restoreSystemProxy = restore
		return nil
	}
	if main.restoreSystemProxy != nil {
		if err := main.restoreSystemProxy(); err != nil {
			return err
		}
		main.restoreSystemProxy = nil
	}
	return nil
}

func sectionIcon(index int) fyne.Resource {
	switch index {
	case 0:
		return theme.MediaPlayIcon()
	case 1:
		return theme.DocumentIcon()
	case 2:
		return theme.ListIcon()
	case 3:
		return theme.SettingsIcon()
	default:
		return theme.InfoIcon()
	}
}
