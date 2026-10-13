package gui

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"example.com/vclient/internal/app"
	"example.com/vclient/internal/config"
	"example.com/vclient/internal/core"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestProfileListItemUsesTwoLineRow(t *testing.T) {
	item := newProfileListItem()
	row, ok := item.(*fyne.Container)
	if !ok {
		t.Fatalf("profile list item type = %T, want *fyne.Container", item)
	}
	if len(row.Objects) != 2 {
		t.Fatalf("profile list item has %d objects, want title and details", len(row.Objects))
	}
	title, titleOK := row.Objects[0].(*widget.Label)
	details, detailsOK := row.Objects[1].(*widget.Label)
	if !titleOK || !detailsOK {
		t.Fatalf("profile list item children = %T and %T, want labels", row.Objects[0], row.Objects[1])
	}
	title.SetText("VLESS profile")
	details.SetText("xhttp · 192.0.2.1:443")
	if title.Text == details.Text {
		t.Fatal("profile title and details should be separate lines")
	}
	if row.MinSize().Height <= title.MinSize().Height {
		t.Fatalf("profile row height = %v, title height = %v; want room for both lines",
			row.MinSize().Height, title.MinSize().Height)
	}
}

func TestMainWindowContainsAllSectionsAndSwitches(t *testing.T) {
	application := test.NewApp()
	defer application.Quit()
	main := newTestWindow(t, application, nil)
	defer func() {
		main.cancel()
		_ = main.service.Shutdown()
	}()
	if got := main.Sections(); len(got) != 5 {
		t.Fatalf("Sections() = %v, want five sections", got)
	}
	for index := range sections {
		main.showSection(index)
		if len(main.content.Objects) != 1 || main.content.Objects[0] == nil {
			t.Fatalf("section %q did not render content", sections[index])
		}
	}
}

func TestConnectedSubscriptionUpdatesButton(t *testing.T) {
	application := test.NewApp()
	defer application.Quit()
	main := newTestWindow(t, application, nil)
	defer func() {
		main.cancel()
		_ = main.service.Shutdown()
	}()
	main.showSection(0)
	if got := main.connect.connect.Text; got != "Подключиться" {
		t.Fatalf("initial connect button text = %q", got)
	}

	test.Tap(main.connect.connect)
	deadline := time.Now().Add(time.Second)
	for main.service.State() != app.Connected && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if main.service.State() != app.Connected {
		t.Fatalf("service state = %s, want connected", main.service.State())
	}
	for time.Now().Before(deadline) {
		fyne.DoAndWait(func() {})
		if main.connect.connect.Text == "Отключиться" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("connect button text = %q, want Отключиться", main.connect.connect.Text)
}

type guiTestRunner struct{}

func (guiTestRunner) Start(context.Context) error { return nil }
func (guiTestRunner) Stop() error                 { return nil }

var _ core.Runner = guiTestRunner{}

func newTestWindow(t *testing.T, application fyne.App, logger *slog.Logger) *MainWindow {
	t.Helper()
	profiles, err := app.NewProfileStore(filepath.Join(t.TempDir(), "profiles.json"))
	if err != nil {
		t.Fatalf("NewProfileStore() error = %v", err)
	}
	profile := config.Profile{
		Name: "gui-test", Protocol: "vless", Address: "proxy.example.com",
		Port: 443, UUID: "01234567-89ab-cdef-0123-456789abcdef",
		Network: "tcp", Security: "none",
	}
	if err := profiles.Add(profile); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	service, err := app.NewService(app.ServiceOptions{
		Profiles: profiles, Settings: app.DefaultSettings(),
		SettingsPath: filepath.Join(t.TempDir(), "settings.json"),
		Logger:       logger,
		RunnerFactory: func([]byte) (core.Runner, error) {
			return guiTestRunner{}, nil
		},
	})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	main := NewWindow(application, service, filepath.Join(t.TempDir(), "settings.json"), "test", nil)
	main.showSection(0)
	return main
}
