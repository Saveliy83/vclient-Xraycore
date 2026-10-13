package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testUUID = "123e4567-e89b-12d3-a456-426614174000"

func TestParseVLESSLink(t *testing.T) {
	tests := []struct {
		name         string
		raw          string
		wantNetwork  string
		wantSecurity string
		wantName     string
		wantAddress  string
		wantPort     int
		wantErr      bool
	}{
		{
			name:         "tcp tls",
			raw:          "vless://" + testUUID + "@proxy.example.com:443?type=tcp&security=tls&sni=example.com&fp=chrome&alpn=h2%2Chttp%2F1.1#main",
			wantNetwork:  "tcp",
			wantSecurity: "tls",
			wantName:     "main",
			wantAddress:  "proxy.example.com",
			wantPort:     443,
		},
		{
			name:         "ws tls",
			raw:          "vless://" + testUUID + "@proxy.example.com:443?type=ws&security=tls&host=cdn.example.com&path=%2Fsocket",
			wantNetwork:  "ws",
			wantSecurity: "tls",
			wantAddress:  "proxy.example.com",
			wantPort:     443,
		},
		{
			name:         "grpc reality",
			raw:          "vless://" + testUUID + "@proxy.example.com:443?type=grpc&security=reality&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=abcd&spx=%2F&serviceName=grpc-service",
			wantNetwork:  "grpc",
			wantSecurity: "reality",
			wantAddress:  "proxy.example.com",
			wantPort:     443,
		},
		{
			name:         "xhttp reality",
			raw:          "vless://" + testUUID + "@proxy.example.com:443?type=xhttp&security=reality&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&mode=packet-up&path=%2Fpush",
			wantNetwork:  "xhttp",
			wantSecurity: "reality",
			wantAddress:  "proxy.example.com",
			wantPort:     443,
		},
		{
			name:         "httpupgrade",
			raw:          "vless://" + testUUID + "@proxy.example.com:443?type=httpupgrade&security=none&path=%2Fupgrade",
			wantNetwork:  "httpupgrade",
			wantSecurity: "none",
			wantAddress:  "proxy.example.com",
			wantPort:     443,
		},
		{
			name:         "IPv6",
			raw:          "vless://" + testUUID + "@[2001:db8::1]:8443?type=tcp&security=none",
			wantNetwork:  "tcp",
			wantSecurity: "none",
			wantName:     "2001:db8::1",
			wantAddress:  "2001:db8::1",
			wantPort:     8443,
		},
		{
			name:         "decoded name with spaces and Cyrillic",
			raw:          "vless://" + testUUID + "@proxy.example.com:443?type=tcp&security=none#%D0%9C%D0%BE%D0%B9%20%D0%BF%D1%80%D0%BE%D1%84%D0%B8%D0%BB%D1%8C",
			wantNetwork:  "tcp",
			wantSecurity: "none",
			wantName:     "Мой профиль",
			wantAddress:  "proxy.example.com",
			wantPort:     443,
		},
		{name: "missing port", raw: "vless://" + testUUID + "@proxy.example.com?security=none", wantErr: true},
		{name: "invalid UUID", raw: "vless://invalid@proxy.example.com:443?security=none", wantErr: true},
		{name: "unknown security", raw: "vless://" + testUUID + "@proxy.example.com:443?security=unknown", wantErr: true},
		{name: "empty host", raw: "vless://" + testUUID + "@:443?security=none", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile, err := ParseVLESSLink(test.raw)
			if (err != nil) != test.wantErr {
				t.Fatalf("ParseVLESSLink() error = %v, wantErr %t", err, test.wantErr)
			}
			if err != nil {
				return
			}
			if profile.Network != test.wantNetwork || profile.Security != test.wantSecurity ||
				profile.Address != test.wantAddress || profile.Port != test.wantPort {
				t.Errorf("parsed profile has unexpected connection settings: %+v", profile)
			}
			if test.wantName != "" && profile.Name != test.wantName {
				t.Errorf("profile name = %q, want %q", profile.Name, test.wantName)
			}
		})
	}
}

func TestParseVLESSLinkMLKEMAndXHTTPPadding(t *testing.T) {
	x25519Key := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	mldsa65Key := base64.RawURLEncoding.EncodeToString(make([]byte, 1952))
	encryption := "mlkem768x25519plus.native.0rtt." + x25519Key
	raw := "vless://" + testUUID + "@proxy.example.com:443?type=xhttp&security=reality" +
		"&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=93&sni=example.com" +
		"&encryption=" + encryption + "&pqv=" + mldsa65Key + "&mode=auto&path=%2Fmedia&x_padding_bytes=100-1000"

	profile, err := ParseVLESSLink(raw)
	if err != nil {
		t.Fatalf("ParseVLESSLink() error = %v", err)
	}
	if profile.Encryption != encryption {
		t.Error("parsed VLESS encryption did not preserve the encryption parameter")
	}
	if profile.Reality.MLDSA65Verify != mldsa65Key {
		t.Error("parsed REALITY ML-DSA-65 key did not preserve the pqv parameter")
	}
	if profile.XHTTP.XPaddingBytes != "100-1000" {
		t.Errorf("XPaddingBytes = %q, want 100-1000", profile.XHTTP.XPaddingBytes)
	}
	if len(profile.Warnings) != 0 {
		t.Errorf("Warnings = %#v, want no warnings for supported parameters", profile.Warnings)
	}
}

func TestParseVLESSLinkXHTTPExtra(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		wantMode    string
		wantPadding string
		wantErr     bool
	}{
		{
			name:        "reads XHTTP fields from extra JSON",
			query:       "extra=%7B%22mode%22%3A%22auto%22%2C%22xPaddingBytes%22%3A%22100-1000%22%7D",
			wantMode:    "auto",
			wantPadding: "100-1000",
		},
		{
			name:        "direct query settings take precedence",
			query:       "mode=packet-up&x_padding_bytes=200-300&extra=%7B%22mode%22%3A%22auto%22%2C%22xPaddingBytes%22%3A%22100-1000%22%7D",
			wantMode:    "packet-up",
			wantPadding: "200-300",
		},
		{
			name:    "invalid extra JSON",
			query:   "extra=not-json",
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw := "vless://" + testUUID + "@proxy.example.com:443?type=xhttp&security=tls&path=%2Fdownload%2Fmedia&" + test.query
			profile, err := ParseVLESSLink(raw)
			if (err != nil) != test.wantErr {
				t.Fatalf("ParseVLESSLink() error = %v, wantErr %t", err, test.wantErr)
			}
			if err != nil {
				return
			}
			if profile.XHTTP.Mode != test.wantMode || profile.XHTTP.XPaddingBytes != test.wantPadding {
				t.Errorf("parsed XHTTP settings = %+v, want mode %q and padding %q",
					profile.XHTTP, test.wantMode, test.wantPadding)
			}
			if len(profile.Warnings) != 0 {
				t.Errorf("Warnings = %#v, want no warnings for recognized XHTTP extra JSON", profile.Warnings)
			}
		})
	}
}

func TestParseVLESSLinkOptionsAndWarnings(t *testing.T) {
	raw := "vless://" + testUUID + "@proxy.example.com:443?type=ws&security=tls&path=%2Fsocket&allowInsecure=true&unknown=z&extra=y#name"
	profile, err := ParseVLESSLink(raw)
	if err != nil {
		t.Fatalf("ParseVLESSLink() error = %v", err)
	}
	if !profile.TLS.AllowInsecure {
		t.Fatal("AllowInsecure = false, want true")
	}
	if len(profile.Warnings) != 2 || !strings.Contains(profile.Warnings[0], "extra") || !strings.Contains(profile.Warnings[1], "unknown") {
		t.Errorf("Warnings = %#v, want sorted warnings for extra and unknown parameters", profile.Warnings)
	}
}

func TestParseVLESSLinkTransportOptions(t *testing.T) {
	tests := []struct {
		name          string
		raw           string
		wantService   string
		wantMultiMode bool
		wantXHTTPMode string
	}{
		{
			name:          "grpc service name and multi mode",
			raw:           "vless://" + testUUID + "@proxy.example.com:443?type=grpc&security=tls&serviceName=edge-service&mode=multi",
			wantService:   "edge-service",
			wantMultiMode: true,
		},
		{
			name:          "xhttp mode",
			raw:           "vless://" + testUUID + "@proxy.example.com:443?type=xhttp&security=tls&mode=stream-up&path=%2Fapi&host=cdn.example.com",
			wantXHTTPMode: "stream-up",
		},
		{
			name: "kcp settings",
			raw:  "vless://" + testUUID + "@proxy.example.com:443?type=mkcp&security=none&mtu=1200&tti=30&headerType=none",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile, err := ParseVLESSLink(test.raw)
			if err != nil {
				t.Fatalf("ParseVLESSLink() error = %v", err)
			}
			if profile.GRPC.ServiceName != test.wantService || profile.GRPC.MultiMode != test.wantMultiMode {
				if test.wantService != "" {
					t.Errorf("gRPC settings = %+v, want serviceName %q and multiMode %t",
						profile.GRPC, test.wantService, test.wantMultiMode)
				}
			}
			if profile.XHTTP.Mode != test.wantXHTTPMode {
				t.Errorf("XHTTP mode = %q, want %q", profile.XHTTP.Mode, test.wantXHTTPMode)
			}
			if test.name == "kcp settings" && (profile.Network != "kcp" || profile.KCP.MTU != 1200 ||
				profile.KCP.TTI != 30 || profile.KCP.HeaderType != "none") {
				t.Errorf("parsed KCP settings = %+v, network = %q", profile.KCP, profile.Network)
			}
		})
	}
}

func TestParseVLESSLinkRejectsInvalidOptions(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "unsupported encryption",
			raw:  "vless://" + testUUID + "@proxy.example.com:443?security=none&encryption=aes",
		},
		{
			name: "invalid allowInsecure value",
			raw:  "vless://" + testUUID + "@proxy.example.com:443?security=none&allowInsecure=yes",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseVLESSLink(test.raw); err == nil {
				t.Fatal("ParseVLESSLink() error = nil, want invalid option error")
			}
		})
	}
}

func TestParseVLESSLinkTestData(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "links.txt"))
	if err != nil {
		t.Fatalf("read links test data: %v", err)
	}
	profiles, parseErrors := ParseSubscription(data)
	if len(parseErrors) != 0 {
		t.Fatalf("ParseSubscription() errors = %v", parseErrors)
	}
	if len(profiles) == 0 {
		t.Fatal("ParseSubscription() returned no profiles from links.txt")
	}
}

func FuzzParseVLESSLink(f *testing.F) {
	f.Add("vless://" + testUUID + "@example.com:443?security=none#seed")
	f.Add("vless://")
	f.Add("")
	f.Add("not a URL")
	f.Add("vless://" + testUUID + "@[2001:db8::1]:443?type=ws&security=tls")

	f.Fuzz(func(t *testing.T, raw string) {
		_, _ = ParseVLESSLink(raw)
	})
}
