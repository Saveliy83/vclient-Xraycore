package config

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSubscription(t *testing.T) {
	link := "vless://" + testUUID + "@proxy.example.com:443?security=none#one"
	encoded := base64.StdEncoding.EncodeToString([]byte(link))
	rawEncoded := strings.TrimRight(encoded, "=")

	tests := []struct {
		name       string
		body       []byte
		wantCount  int
		wantErrors int
	}{
		{name: "base64", body: []byte(encoded), wantCount: 1},
		{name: "base64 without padding", body: []byte(rawEncoded), wantCount: 1},
		{name: "plain text", body: []byte(link + "\n" + link), wantCount: 2},
		{name: "mixed valid and invalid lines", body: []byte(link + "\ninvalid-line\n" + link), wantCount: 2, wantErrors: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profiles, parseErrors := ParseSubscription(test.body)
			if len(profiles) != test.wantCount {
				t.Errorf("ParseSubscription() profiles = %d, want %d", len(profiles), test.wantCount)
			}
			if len(parseErrors) != test.wantErrors {
				t.Errorf("ParseSubscription() errors = %d, want %d: %v", len(parseErrors), test.wantErrors, parseErrors)
			}
		})
	}
}

func TestParseSubscriptionURLSafeWrappedBase64(t *testing.T) {
	link := "vless://" + testUUID + "@proxy.example.com:443?security=none#url-safe"
	encoded := base64.RawURLEncoding.EncodeToString([]byte(link))
	body := []byte(encoded[:len(encoded)/2] + "\n" + encoded[len(encoded)/2:])
	profiles, parseErrors := ParseSubscription(body)
	if len(parseErrors) != 0 || len(profiles) != 1 {
		t.Fatalf("ParseSubscription() profiles=%d errors=%v", len(profiles), parseErrors)
	}
	if profiles[0].Name != "url-safe" {
		t.Errorf("profile name = %q, want url-safe", profiles[0].Name)
	}
}

func TestSelectProfileByName(t *testing.T) {
	body := []byte(
		"vless://" + testUUID + "@one.example:443?type=tcp&security=none#first\n" +
			"vless://" + testUUID + "@two.example:443?type=tcp&security=none#selected",
	)
	profiles, parseErrors := ParseSubscription(body)
	if len(parseErrors) != 0 {
		t.Fatalf("ParseSubscription() errors = %v", parseErrors)
	}
	got, err := SelectProfile(profiles, "selected")
	if err != nil {
		t.Fatalf("SelectProfile() error = %v", err)
	}
	if got.Name != "selected" {
		t.Errorf("selected profile = %q, want selected", got.Name)
	}
	if _, err := SelectProfile(profiles, "missing"); err == nil {
		t.Fatal("SelectProfile() error = nil, want profile-not-found error")
	}
}

func TestSubscriptionCacheWriteReadAndCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache", "subscription.json")
	profiles := []Profile{{
		Name: "cached", Protocol: "vless", Address: "proxy.example.com",
		Port: 443, UUID: testUUID, Network: "tcp", Security: "none",
	}}
	if err := SaveSubscriptionCache(path, profiles); err != nil {
		t.Fatalf("SaveSubscriptionCache() error = %v", err)
	}
	got, err := LoadSubscriptionCache(path)
	if err != nil {
		t.Fatalf("LoadSubscriptionCache() error = %v", err)
	}
	if len(got) != 1 || got[0].Name != "cached" {
		t.Fatalf("LoadSubscriptionCache() = %#v, want cached profile", got)
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatalf("write corrupt cache: %v", err)
	}
	if _, err := LoadSubscriptionCache(path); err == nil || !strings.Contains(err.Error(), "decode subscription cache") {
		t.Fatalf("LoadSubscriptionCache() error = %v, want corrupt-cache error", err)
	}
}

func TestLoadSubscriptionCacheRejectsOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "subscription.json")
	if err := os.WriteFile(path, make([]byte, maxSubscriptionCacheSize+1), 0o600); err != nil {
		t.Fatalf("write oversized cache: %v", err)
	}
	if _, err := LoadSubscriptionCache(path); err == nil || !strings.Contains(err.Error(), "maximum allowed size") {
		t.Fatalf("LoadSubscriptionCache() error = %v, want size-limit error", err)
	}
}

func TestParseSubscriptionBase64TestData(t *testing.T) {
	profiles, parseErrors := loadSubscriptionFixture(t)
	if len(parseErrors) != 0 {
		t.Fatalf("ParseSubscription() errors = %v", parseErrors)
	}
	if len(profiles) != 1 {
		t.Fatalf("ParseSubscription() profiles = %d, want 1", len(profiles))
	}
}

func TestFetchSubscription(t *testing.T) {
	link := "vless://" + testUUID + "@proxy.example.com:443?security=none#fetched"
	tests := []struct {
		name      string
		status    int
		body      string
		wantCount int
		wantErr   bool
	}{
		{name: "200", status: http.StatusOK, body: link, wantCount: 1},
		{name: "404", status: http.StatusNotFound, body: "missing", wantErr: true},
		{name: "too large", status: http.StatusOK, body: strings.Repeat("x", maxSubscriptionSize+1), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.WriteHeader(test.status)
				_, _ = fmt.Fprint(writer, test.body)
			}))
			defer server.Close()

			profiles, err := FetchSubscription(context.Background(), server.URL, server.Client())
			if (err != nil) != test.wantErr {
				t.Fatalf("FetchSubscription() error = %v, wantErr %t", err, test.wantErr)
			}
			if len(profiles) != test.wantCount {
				t.Errorf("FetchSubscription() profiles = %d, want %d", len(profiles), test.wantCount)
			}
		})
	}
}

func loadSubscriptionFixture(t *testing.T) ([]Profile, []error) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "subscription.b64"))
	if err != nil {
		t.Fatalf("read subscription fixture: %v", err)
	}
	return ParseSubscription(body)
}
