package gui

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"example.com/vclient/internal/app"
	"example.com/vclient/internal/config"
)

func ValidateProxyAddress(address string) error {
	host, portText, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return fmt.Errorf("invalid proxy address: %w", err)
	}
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("invalid proxy address: host must not be empty")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("invalid proxy address: port must be between 1 and 65535")
	}
	return nil
}

func ValidateSettings(settings app.Settings) error {
	if err := ValidateProxyAddress(settings.SOCKSAddr); err != nil {
		return fmt.Errorf("validate SOCKS5 address: %w", err)
	}
	if err := ValidateProxyAddress(settings.HTTPAddr); err != nil {
		return fmt.Errorf("validate HTTP address: %w", err)
	}
	return nil
}

func ValidateProfileForm(profile config.Profile) error {
	if err := profile.Validate(); err != nil {
		return fmt.Errorf("validate profile form: %w", err)
	}
	return nil
}
