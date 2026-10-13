package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Settings struct {
	Theme          string `json:"theme"`
	SOCKSAddr      string `json:"socks_addr"`
	HTTPAddr       string `json:"http_addr"`
	LogLevel       string `json:"log_level"`
	SystemProxy    bool   `json:"system_proxy"`
	AutoConnect    bool   `json:"auto_connect"`
	MinimizeToTray bool   `json:"minimize_to_tray"`
	AutoStart      bool   `json:"auto_start"`
	FullTunnel     bool   `json:"full_tunnel"`
	BlockIPv6      bool   `json:"block_ipv6"`
	LastProfile    string `json:"last_profile"`
}

func DefaultSettings() Settings {
	return Settings{
		Theme:     "system",
		SOCKSAddr: "127.0.0.1:1080",
		HTTPAddr:  "127.0.0.1:1081",
		LogLevel:  "info",
		BlockIPv6: true,
	}
}

func LoadSettings() (Settings, []string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return DefaultSettings(), nil, fmt.Errorf("resolve user config directory: %w", err)
	}
	return LoadSettingsFrom(filepath.Join(configDir, "vclient", "settings.json"))
}

func LoadSettingsFrom(path string) (Settings, []string, error) {
	defaults := DefaultSettings()
	body, err := os.ReadFile(path) // #nosec G304 -- path is the application settings file.
	if errors.Is(err, os.ErrNotExist) {
		return defaults, nil, nil
	}
	if err != nil {
		return defaults, []string{fmt.Sprintf("could not read settings %q; defaults are being used: %v", path, err)}, nil
	}
	var loaded Settings
	if err := json.Unmarshal(body, &loaded); err != nil {
		return defaults, []string{fmt.Sprintf("settings file is invalid; defaults are being used: %v", err)}, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return defaults, []string{fmt.Sprintf("settings file is invalid; defaults are being used: %v", err)}, nil
	}
	if _, present := fields["block_ipv6"]; !present {
		loaded.BlockIPv6 = defaults.BlockIPv6
	}

	var warnings []string
	if loaded.Theme == "" {
		loaded.Theme = defaults.Theme
	} else if loaded.Theme != "system" && loaded.Theme != "dark" && loaded.Theme != "light" {
		loaded.Theme = defaults.Theme
		warnings = append(warnings, "invalid theme; using system")
	}
	if loaded.SOCKSAddr == "" {
		loaded.SOCKSAddr = defaults.SOCKSAddr
	}
	if err := validateListenAddress(loaded.SOCKSAddr); err != nil {
		loaded.SOCKSAddr = defaults.SOCKSAddr
		warnings = append(warnings, "invalid SOCKS address; using default")
	}
	if loaded.HTTPAddr == "" {
		loaded.HTTPAddr = defaults.HTTPAddr
	}
	if err := validateListenAddress(loaded.HTTPAddr); err != nil {
		loaded.HTTPAddr = defaults.HTTPAddr
		warnings = append(warnings, "invalid HTTP address; using default")
	}
	if loaded.LogLevel == "" {
		loaded.LogLevel = defaults.LogLevel
	}
	if _, err := parseLogLevel(loaded.LogLevel); err != nil {
		loaded.LogLevel = defaults.LogLevel
		warnings = append(warnings, "invalid log level; using info")
	}
	return loaded, warnings, nil
}

func SaveSettings(settings Settings) error {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("resolve user config directory: %w", err)
	}
	return SaveSettingsTo(filepath.Join(configDir, "vclient", "settings.json"), settings)
}

func SaveSettingsTo(path string, settings Settings) (result error) {
	if err := validateSettings(settings); err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	body, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create settings directory: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".settings-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary settings file: %w", err)
	}
	tempPath := temp.Name()
	closed := false
	renamed := false
	defer func() {
		if !closed {
			result = errors.Join(result, temp.Close())
		}
		if !renamed {
			if removeErr := os.Remove(tempPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				result = errors.Join(result, fmt.Errorf("remove temporary settings file: %w", removeErr))
			}
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return fmt.Errorf("set settings file permissions: %w", err)
	}
	if _, err := temp.Write(body); err != nil {
		return fmt.Errorf("write temporary settings file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("sync temporary settings file: %w", err)
	}
	if err := temp.Close(); err != nil {
		closed = true
		return fmt.Errorf("close temporary settings file: %w", err)
	}
	closed = true
	if err := restrictAppFile(tempPath); err != nil {
		return fmt.Errorf("secure temporary settings file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace settings file: %w", err)
	}
	renamed = true
	return nil
}

func validateSettings(settings Settings) error {
	if settings.Theme != "system" && settings.Theme != "dark" && settings.Theme != "light" {
		return fmt.Errorf("validate theme: %w", errors.New("theme must be system, dark, or light"))
	}
	if err := validateListenAddress(settings.SOCKSAddr); err != nil {
		return fmt.Errorf("validate SOCKS address: %w", err)
	}
	if err := validateListenAddress(settings.HTTPAddr); err != nil {
		return fmt.Errorf("validate HTTP address: %w", err)
	}
	if _, err := parseLogLevel(settings.LogLevel); err != nil {
		return fmt.Errorf("validate log level: %w", err)
	}
	return nil
}

func validateListenAddress(address string) error {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("split host and port: %w", err)
	}
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("validate listen address: %w", errors.New("host must not be empty"))
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("validate listen address port %q: %w", portText, errors.New("port must be between 1 and 65535"))
	}
	return nil
}

func AutoStartCommand(executablePath string) string {
	return `"` + strings.ReplaceAll(executablePath, `"`, `\"`) + `"`
}
