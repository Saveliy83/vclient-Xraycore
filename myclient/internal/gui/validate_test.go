package gui

import (
	"testing"

	"example.com/vclient/internal/app"
	"example.com/vclient/internal/config"
)

func TestValidateProxyAddress(t *testing.T) {
	tests := []struct {
		address string
		valid   bool
	}{
		{address: "127.0.0.1:1080", valid: true},
		{address: "[::1]:1080", valid: true},
		{address: "localhost:8080", valid: true},
		{address: "localhost", valid: false},
		{address: ":8080", valid: false},
		{address: "localhost:70000", valid: false},
	}
	for _, test := range tests {
		t.Run(test.address, func(t *testing.T) {
			err := ValidateProxyAddress(test.address)
			if (err == nil) != test.valid {
				t.Errorf("ValidateProxyAddress(%q) error = %v, valid=%t", test.address, err, test.valid)
			}
		})
	}
}

func TestValidateSettings(t *testing.T) {
	settings := app.DefaultSettings()
	if err := ValidateSettings(settings); err != nil {
		t.Fatalf("ValidateSettings() defaults error = %v", err)
	}
	settings.SOCKSAddr = "bad"
	if err := ValidateSettings(settings); err == nil {
		t.Fatal("ValidateSettings() invalid SOCKS address error = nil")
	}
}

func TestValidateProfileForm(t *testing.T) {
	profile := config.Profile{
		Name: "form", Protocol: "vless", Address: "proxy.example.com",
		Port: 443, UUID: "01234567-89ab-cdef-0123-456789abcdef",
		Network: "tcp", Security: "none",
	}
	if err := ValidateProfileForm(profile); err != nil {
		t.Fatalf("ValidateProfileForm() valid profile error = %v", err)
	}
	profile.Port = 0
	if err := ValidateProfileForm(profile); err == nil {
		t.Fatal("ValidateProfileForm() invalid profile error = nil")
	}
}
