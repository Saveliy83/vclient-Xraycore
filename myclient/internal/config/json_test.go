package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProfileFromJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.json")
	data := []byte(`{"name":"json","protocol":"vless","address":"proxy.example.com","port":443,"uuid":"123e4567-e89b-12d3-a456-426614174000","network":"tcp","security":"none"}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write profile JSON: %v", err)
	}
	profile, err := LoadProfileFromJSON(path)
	if err != nil {
		t.Fatalf("LoadProfileFromJSON() error = %v", err)
	}
	if profile.Name != "json" || profile.Protocol != "vless" {
		t.Errorf("loaded profile = %+v", profile)
	}
}

func TestLoadProfileFromJSONRejectsInvalidProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.json")
	if err := os.WriteFile(path, []byte(`{"name":"invalid"}`), 0o600); err != nil {
		t.Fatalf("write profile JSON: %v", err)
	}
	if _, err := LoadProfileFromJSON(path); err == nil {
		t.Fatal("LoadProfileFromJSON() error = nil, want validation error")
	}
}
