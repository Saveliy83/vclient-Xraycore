package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSettingsDefaultsAndInvalidAddressFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	settings, warnings, err := LoadSettingsFrom(path)
	if err != nil {
		t.Fatalf("LoadSettingsFrom() error = %v", err)
	}
	if settings != DefaultSettings() || len(warnings) != 0 {
		t.Fatalf("LoadSettingsFrom() = %#v warnings=%v, want defaults", settings, warnings)
	}

	if err := os.WriteFile(path, []byte(`{"theme":"dark","socks_addr":"bad","http_addr":"127.0.0.1:8080","log_level":"debug"}`), 0o600); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	settings, warnings, err = LoadSettingsFrom(path)
	if err != nil {
		t.Fatalf("LoadSettingsFrom() invalid address error = %v", err)
	}
	if settings.SOCKSAddr != DefaultSettings().SOCKSAddr || settings.HTTPAddr != "127.0.0.1:8080" ||
		settings.Theme != "dark" || settings.LogLevel != "debug" {
		t.Fatalf("settings after address fallback = %#v", settings)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "SOCKS") {
		t.Fatalf("warnings = %v, want invalid SOCKS address warning", warnings)
	}
}

func TestSettingsRoundTripAndInvalidThemeFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	want := DefaultSettings()
	want.Theme = "light"
	want.SOCKSAddr = "[::1]:2080"
	want.HTTPAddr = "localhost:2081"
	want.LogLevel = "warn"
	want.SystemProxy = true
	want.AutoConnect = true
	want.MinimizeToTray = true
	want.AutoStart = true
	want.FullTunnel = true
	want.BlockIPv6 = false
	want.LastProfile = "work"
	if err := SaveSettingsTo(path, want); err != nil {
		t.Fatalf("SaveSettingsTo() error = %v", err)
	}
	got, warnings, err := LoadSettingsFrom(path)
	if err != nil {
		t.Fatalf("LoadSettingsFrom() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) || len(warnings) != 0 {
		t.Fatalf("settings round trip = %#v warnings=%v, want %#v", got, warnings, want)
	}

	if err := os.WriteFile(path, []byte(`{"theme":"neon","socks_addr":"127.0.0.1:1080","http_addr":"127.0.0.1:1081","log_level":"info"}`), 0o600); err != nil {
		t.Fatalf("write invalid theme settings: %v", err)
	}
	got, warnings, err = LoadSettingsFrom(path)
	if err != nil {
		t.Fatalf("LoadSettingsFrom() invalid theme error = %v", err)
	}
	if got.Theme != "system" || len(warnings) != 1 {
		t.Fatalf("invalid theme fallback = %#v warnings=%v", got, warnings)
	}
}

func TestAutoStartCommandQuotesExecutablePath(t *testing.T) {
	got := AutoStartCommand(`C:\Program Files\vclient\vclient-gui.exe`)
	want := `"C:\Program Files\vclient\vclient-gui.exe"`
	if got != want {
		t.Fatalf("AutoStartCommand() = %q, want %q", got, want)
	}
}

func TestDefaultSettingsDisableOptionalStartupBehavior(t *testing.T) {
	settings := DefaultSettings()
	if settings.MinimizeToTray || settings.AutoStart || settings.FullTunnel || !settings.BlockIPv6 {
		t.Fatalf("optional defaults = tray:%t autostart:%t full-tunnel:%t block-ipv6:%t, want safe IPv6 blocking and other options disabled",
			settings.MinimizeToTray, settings.AutoStart, settings.FullTunnel, settings.BlockIPv6)
	}
}

func TestLegacySettingsDefaultToBlockingIPv6(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"theme":"system","socks_addr":"127.0.0.1:1080","http_addr":"127.0.0.1:1081","log_level":"info"}`), 0o600); err != nil {
		t.Fatalf("write legacy settings: %v", err)
	}
	got, _, err := LoadSettingsFrom(path)
	if err != nil {
		t.Fatalf("LoadSettingsFrom() error = %v", err)
	}
	if !got.BlockIPv6 {
		t.Fatal("legacy settings did not enable safe IPv6 blocking by default")
	}
}

func TestSettingsRejectInvalidListenAddress(t *testing.T) {
	settings := DefaultSettings()
	settings.HTTPAddr = "localhost:not-a-port"
	if err := SaveSettingsTo(filepath.Join(t.TempDir(), "settings.json"), settings); err == nil {
		t.Fatal("SaveSettingsTo() invalid address error = nil")
	}
}
