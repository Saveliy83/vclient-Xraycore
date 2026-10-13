package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
)

func (main *MainWindow) buildMenu() *fyne.MainMenu {
	quit := fyne.NewMenuItem("Выход", main.closeIntercept)
	quit.Shortcut = &desktop.CustomShortcut{
		KeyName: fyne.KeyQ, Modifier: fyne.KeyModifierControl,
	}
	about := fyne.NewMenuItem("О программе", func() { main.showSection(4) })
	about.Icon = theme.InfoIcon()
	return fyne.NewMainMenu(
		fyne.NewMenu("Файл",
			fyne.NewMenuItem("Подключиться", func() { main.connectSelected() }),
			fyne.NewMenuItem("Отключиться", func() { main.disconnect() }),
			fyne.NewMenuItemSeparator(),
			quit,
		),
		fyne.NewMenu("Вид",
			fyne.NewMenuItem("Системная тема", func() { main.setTheme("system") }),
			fyne.NewMenuItem("Светлая тема", func() { main.setTheme("light") }),
			fyne.NewMenuItem("Тёмная тема", func() { main.setTheme("dark") }),
		),
		fyne.NewMenu("Справка",
			about,
		),
	)
}

func (main *MainWindow) connectSelected() {
	if main.connect == nil {
		main.showSection(0)
	}
	main.connectConnection()
}

func (main *MainWindow) connectConnection() {
	if main.connect != nil {
		main.connect.toggle()
	}
}

func (main *MainWindow) disconnect() {
	main.toggleConnection()
}

func (main *MainWindow) setTheme(name string) {
	settings := main.service.Settings()
	settings.Theme = name
	if err := main.saveSettings(settings); err != nil {
		fyne.Do(func() { main.showError(err) })
		return
	}
	main.applyTheme(name)
	if main.settings != nil {
		main.settings.refresh()
	}
}

func (main *MainWindow) applyTheme(name string) {
	main.app.Settings().SetTheme(ThemeFor(name))
}
