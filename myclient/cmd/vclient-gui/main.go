package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"example.com/vclient/internal/app"
	"example.com/vclient/internal/gui"
	"example.com/vclient/internal/logger"
	"example.com/vclient/internal/tunmode"
	fyneapp "fyne.io/fyne/v2/app"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("vclient GUI stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("resolve user config directory: %w", err)
	}
	dataDir := filepath.Join(configDir, "vclient")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("create application data directory: %w", err)
	}
	settingsPath := filepath.Join(dataDir, "settings.json")
	settings, warnings, err := app.LoadSettingsFrom(settingsPath)
	if err != nil {
		return fmt.Errorf("load GUI settings: %w", err)
	}
	log, closer, err := logger.New(settings.LogLevel, "")
	if err != nil {
		return fmt.Errorf("create GUI logger: %w", err)
	}
	for _, warning := range warnings {
		log.Warn(warning)
	}
	tunnelManager, err := tunmode.NewManager(tunmode.Options{Logger: log})
	if err != nil {
		_ = closer.Close()
		return fmt.Errorf("create full-tunnel manager: %w", err)
	}
	if err := tunnelManager.RecoverOrphan(); err != nil {
		log.Warn("automatic recovery of previous full-tunnel state failed", "error", err)
	}
	profiles, profileErr := app.OpenProfileStore()
	if profileErr != nil {
		if profiles == nil {
			_ = closer.Close()
			return fmt.Errorf("open GUI profile store: %w", profileErr)
		}
		log.Error("profile store has invalid data; it was preserved")
	}
	service, err := app.NewService(app.ServiceOptions{
		Profiles: profiles, Settings: settings, SettingsPath: settingsPath, Logger: log,
		TunnelManager: tunnelManager,
	})
	if err != nil {
		_ = closer.Close()
		return fmt.Errorf("create GUI service: %w", err)
	}
	application := fyneapp.NewWithID("example.com/vclient")
	application.SetIcon(gui.IconResource())
	application.Settings().SetTheme(gui.ThemeFor(settings.Theme))
	mainWindow := gui.NewWindow(application, service, settingsPath, version, closer)
	mainWindow.Show()
	if settings.AutoConnect && settings.LastProfile != "" {
		mainWindow.ConnectLastProfile(settings.LastProfile)
	}
	application.Run()
	if err := service.Shutdown(); err != nil {
		return fmt.Errorf("shutdown GUI service: %w", err)
	}
	return nil
}

func init() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
}
