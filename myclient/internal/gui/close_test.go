package gui

import (
	"testing"

	"example.com/vclient/internal/app"
)

func TestActionOnClose(t *testing.T) {
	tests := []struct {
		name          string
		minimize      bool
		trayAvailable bool
		want          CloseAction
	}{
		{name: "close when disabled", trayAvailable: true, want: CloseApplication},
		{name: "hide when enabled", minimize: true, trayAvailable: true, want: HideWindowToTray},
		{name: "close without tray support", minimize: true, want: CloseApplication},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ActionOnClose(app.Settings{MinimizeToTray: test.minimize}, test.trayAvailable)
			if got != test.want {
				t.Fatalf("ActionOnClose() = %v, want %v", got, test.want)
			}
		})
	}
}
