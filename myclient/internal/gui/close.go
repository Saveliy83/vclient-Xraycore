package gui

import "example.com/vclient/internal/app"

type CloseAction int

const (
	CloseApplication CloseAction = iota
	HideWindowToTray
)

func ActionOnClose(settings app.Settings, trayAvailable bool) CloseAction {
	if settings.MinimizeToTray && trayAvailable {
		return HideWindowToTray
	}
	return CloseApplication
}
