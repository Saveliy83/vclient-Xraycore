package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// LoadProfileFromJSON загружает и проверяет профиль в упрощённом формате vclient.
func LoadProfileFromJSON(path string) (Profile, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- path is explicitly selected by the local user.
	if err != nil {
		return Profile{}, fmt.Errorf("read profile JSON %q: %w", path, err)
	}
	var profile Profile
	if err := json.Unmarshal(data, &profile); err != nil {
		return Profile{}, fmt.Errorf("decode profile JSON %q: %w", path, err)
	}
	if err := profile.Validate(); err != nil {
		return Profile{}, fmt.Errorf("validate profile JSON %q: %w", path, err)
	}
	return profile, nil
}
