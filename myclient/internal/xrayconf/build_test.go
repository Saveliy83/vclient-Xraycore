package xrayconf

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"example.com/vclient/internal/config"
	"example.com/vclient/internal/core"
	dnsconfig "example.com/vclient/internal/dns"
	"example.com/vclient/internal/router"
)

const goldenUUID = "123e4567-e89b-12d3-a456-426614174000"
const goldenRealityPublicKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

var updateGolden = flag.Bool("update", false, "update Xray config golden files")

func goldenProfile(security string) config.Profile {
	return config.Profile{
		Name:     "golden",
		Protocol: "vless",
		Address:  "proxy.example.com",
		Port:     443,
		UUID:     goldenUUID,
		Network:  "tcp",
		Security: security,
		TLS: config.TLS{
			ServerName:  "sni.example.com",
			ALPN:        []string{"h2", "http/1.1"},
			Fingerprint: "chrome",
		},
	}
}

func goldenTransportProfile(network, security string) config.Profile {
	profile := goldenProfile(security)
	profile.Network = network
	switch network {
	case "ws":
		profile.WS = config.WS{Path: "/socket", Host: "cdn.example.com"}
	case "httpupgrade":
		profile.HTTPUpgrade = config.HTTPUpgrade{Path: "/upgrade", Host: "upgrade.example.com"}
	}
	return profile
}

func goldenOptions() Options {
	return Options{
		SOCKSListen: "127.0.0.1:1080",
		HTTPListen:  "127.0.0.1:1081",
	}
}

func goldenRealityProfile(flow string) config.Profile {
	profile := goldenProfile("reality")
	profile.Flow = flow
	profile.Reality = config.Reality{
		PublicKey:   goldenRealityPublicKey,
		ShortID:     "0123456789abcdef",
		SpiderX:     "/",
		ServerName:  "sni.example.com",
		Fingerprint: "chrome",
	}
	return profile
}

func goldenGRPCProfile(security string) config.Profile {
	profile := goldenProfile(security)
	profile.Network = "grpc"
	profile.GRPC = config.GRPC{ServiceName: "grpc-service", MultiMode: true}
	if security == "reality" {
		profile.Reality = goldenRealityProfile("").Reality
	}
	return profile
}

func goldenXHTTPProfile(security, mode string) config.Profile {
	profile := goldenProfile(security)
	profile.Network = "xhttp"
	profile.XHTTP = config.XHTTP{
		Path:    "/xhttp",
		Host:    "cdn.example.com",
		Mode:    mode,
		Headers: map[string]string{"X-Test": "value"},
	}
	if security == "reality" {
		profile.Reality = goldenRealityProfile("").Reality
	}
	return profile
}

func goldenKCPProfile() config.Profile {
	profile := goldenProfile("none")
	profile.Network = "kcp"
	profile.KCP = config.KCP{MTU: 1200, TTI: 30}
	return profile
}

func TestBuildXrayJSONGolden(t *testing.T) {
	tests := []struct {
		name    string
		profile config.Profile
	}{
		{name: "tcp-tls", profile: goldenProfile("tls")},
		{name: "tcp-none", profile: goldenProfile("none")},
		{name: "ws-tls", profile: goldenTransportProfile("ws", "tls")},
		{name: "httpupgrade-tls", profile: goldenTransportProfile("httpupgrade", "tls")},
		{name: "tcp-reality-vision", profile: goldenRealityProfile("xtls-rprx-vision")},
		{name: "tcp-reality", profile: goldenRealityProfile("")},
		{name: "grpc-reality", profile: goldenGRPCProfile("reality")},
		{name: "grpc-tls", profile: goldenGRPCProfile("tls")},
		{name: "xhttp-reality-auto", profile: goldenXHTTPProfile("reality", "auto")},
		{name: "xhttp-tls-packet-up", profile: goldenXHTTPProfile("tls", "packet-up")},
		{name: "kcp-none", profile: goldenKCPProfile()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := goldenOptions()
			options.AllowExperimental = test.profile.Network == "kcp"
			got, err := BuildXrayJSON(test.profile, options)
			if err != nil {
				t.Fatalf("BuildXrayJSON() error = %v", err)
			}
			path := filepath.Join("testdata", "golden", test.name+".json")
			if *updateGolden {
				if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
					t.Fatalf("create golden directory: %v", err)
				}
				if err := os.WriteFile(path, got, 0o600); err != nil {
					t.Fatalf("write golden file: %v", err)
				}
			}
			want, err := os.ReadFile(path) // #nosec G304 -- golden path is constructed from fixed test cases.
			if err != nil {
				t.Fatalf("read golden file: %v (run with -update to create it)", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("BuildXrayJSON() differs from %s\nGot:\n%s", path, got)
			}
		})
	}
}

func TestBuildXrayJSONRoutingAndDNSGolden(t *testing.T) {
	options := goldenOptions()
	options.Rules = []router.Rule{
		{
			Type:        "field",
			Domain:      []string{"domain:example.cn"},
			OutboundTag: "direct",
		},
		{
			Type:        "field",
			IP:          []string{"192.0.2.0/24"},
			Port:        "80,443",
			Network:     []string{"tcp"},
			OutboundTag: "block",
		},
	}

	options.DNS = &dnsconfig.Config{Servers: []dnsconfig.Server{
		{Address: "1.1.1.1:53", OutboundTag: "direct", Domains: []string{"full:local.example"}},
		{Address: "https://dns.google/dns-query", OutboundTag: "proxy"},
	}}
	got, err := BuildXrayJSON(goldenProfile("tls"), options)
	if err != nil {
		t.Fatalf("BuildXrayJSON() error = %v", err)
	}
	path := filepath.Join("testdata", "golden", "routing-dns.json")
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("create golden directory: %v", err)
		}
		if err := os.WriteFile(path, got, 0o600); err != nil {
			t.Fatalf("write golden file: %v", err)
		}
	}
	want, err := os.ReadFile(path) // #nosec G304 -- golden path is constructed from fixed test cases.
	if err != nil {
		t.Fatalf("read golden file: %v (run with -update to create it)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("BuildXrayJSON() differs from %s\nGot:\n%s", path, got)
	}
}

func TestBuildXrayJSONMLKEMAndXHTTPPadding(t *testing.T) {
	profile := goldenXHTTPProfile("reality", "auto")
	profile.Encryption = "mlkem768x25519plus.native.0rtt.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	profile.Reality.MLDSA65Verify = base64.RawURLEncoding.EncodeToString(make([]byte, 1952))
	profile.XHTTP.XPaddingBytes = "100-1000"
	data, err := BuildXrayJSON(profile, goldenOptions())
	if err != nil {
		t.Fatalf("BuildXrayJSON() error = %v", err)
	}

	var generated struct {
		Outbounds []struct {
			Settings struct {
				VNext []struct {
					Users []struct {
						Encryption string `json:"encryption"`
					} `json:"users"`
				} `json:"vnext"`
			} `json:"settings"`
			StreamSettings struct {
				RealitySettings struct {
					MLDSA65Verify string `json:"mldsa65Verify"`
				} `json:"realitySettings"`
				XHTTPSettings struct {
					XPaddingBytes string `json:"xPaddingBytes"`
				} `json:"xhttpSettings"`
			} `json:"streamSettings"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &generated); err != nil {
		t.Fatalf("unmarshal generated config: %v", err)
	}
	if got := generated.Outbounds[0].Settings.VNext[0].Users[0].Encryption; got != profile.Encryption {
		t.Error("generated config did not preserve VLESS encryption")
	}
	if got := generated.Outbounds[0].StreamSettings.RealitySettings.MLDSA65Verify; got != profile.Reality.MLDSA65Verify {
		t.Error("generated config did not preserve REALITY ML-DSA-65 verification key")
	}
	if got := generated.Outbounds[0].StreamSettings.XHTTPSettings.XPaddingBytes; got != "100-1000" {
		t.Errorf("xPaddingBytes = %q, want 100-1000", got)
	}
}

func TestBuildXrayJSONTunInbound(t *testing.T) {
	data, err := BuildXrayJSON(goldenProfile("tls"), Options{
		SOCKSListen: "127.0.0.1:1080",
		HTTPListen:  "127.0.0.1:1081",
		TunInbound:  true,
	})
	if err != nil {
		t.Fatalf("BuildXrayJSON() error = %v", err)
	}
	var document struct {
		Inbounds []struct {
			Protocol string         `json:"protocol"`
			Port     int            `json:"port"`
			Settings map[string]any `json:"settings"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("unmarshal generated JSON: %v", err)
	}
	if len(document.Inbounds) != 3 || document.Inbounds[2].Protocol != "tun" ||
		document.Inbounds[2].Port != 0 || document.Inbounds[2].Settings["name"] != "vclient-tun" {
		t.Fatalf("generated TUN inbound = %+v", document.Inbounds)
	}
	if err := core.ValidateConfigJSON(data); err != nil {
		t.Fatalf("Xray core rejected generated TUN JSON: %v", err)
	}
}

func TestBuildXrayJSONRejectsExperimentalKCPWithoutFlag(t *testing.T) {
	_, err := BuildXrayJSON(goldenKCPProfile(), goldenOptions())
	if err == nil || !strings.Contains(err.Error(), "transport kcp is experimental; enable -allow-experimental") {
		t.Fatalf("BuildXrayJSON() error = %v, want experimental transport rejection", err)
	}
}

func TestBuildXrayJSONRejectsRemovedQUIC(t *testing.T) {
	profile := goldenProfile("none")
	profile.Network = "quic"
	_, err := BuildXrayJSON(profile, goldenOptions())
	if err == nil || !strings.Contains(err.Error(), "transport quic: not supported by linked xray-core version") {
		t.Fatalf("BuildXrayJSON() error = %v, want QUIC unsupported error", err)
	}
}

func TestBuildXrayJSONKCPSettings(t *testing.T) {
	options := goldenOptions()
	options.AllowExperimental = true
	data, err := BuildXrayJSON(goldenKCPProfile(), options)
	if err != nil {
		t.Fatalf("BuildXrayJSON() error = %v", err)
	}
	var document struct {
		Outbounds []struct {
			StreamSettings struct {
				KCPSettings map[string]uint32 `json:"kcpSettings"`
			} `json:"streamSettings"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("unmarshal generated JSON: %v", err)
	}
	if got := document.Outbounds[0].StreamSettings.KCPSettings; got["mtu"] != 1200 || got["tti"] != 30 {
		t.Errorf("KCP settings = %#v, want mtu=1200 and tti=30", got)
	}
}

func TestBuildXrayJSONValidAndHasExpectedTags(t *testing.T) {
	data, err := BuildXrayJSON(goldenProfile("tls"), goldenOptions())
	if err != nil {
		t.Fatalf("BuildXrayJSON() error = %v", err)
	}
	var parsed struct {
		Inbounds []struct {
			Tag string `json:"tag"`
		} `json:"inbounds"`
		Outbounds []struct {
			Tag string `json:"tag"`
		} `json:"outbounds"`
		Routing struct {
			Rules []struct {
				Network     string `json:"network"`
				OutboundTag string `json:"outboundTag"`
			} `json:"rules"`
		} `json:"routing"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("generated JSON is invalid: %v", err)
	}
	if got, want := tagsInbound(parsed.Inbounds), []string{"socks-in", "http-in"}; !reflect.DeepEqual(got, want) {
		t.Errorf("inbound tags = %v, want %v", got, want)
	}
	if got, want := tagsOutbound(parsed.Outbounds), []string{"proxy", "direct", "block"}; !reflect.DeepEqual(got, want) {
		t.Errorf("outbound tags = %v, want %v", got, want)
	}
	if len(parsed.Routing.Rules) != 1 || parsed.Routing.Rules[0].OutboundTag != "proxy" ||
		parsed.Routing.Rules[0].Network != "tcp,udp" {
		t.Errorf("routing rules = %+v, want default outbound proxy", parsed.Routing.Rules)
	}
}

func TestBuildXrayJSONRejectsUnsupportedNetwork(t *testing.T) {
	profile := goldenProfile("none")
	profile.Network = "unknown"
	_, err := BuildXrayJSON(profile, goldenOptions())
	if err == nil || !strings.Contains(err.Error(), "network must be one of") {
		t.Fatalf("BuildXrayJSON() error = %v, want unsupported network validation error", err)
	}
}

func TestBuildXrayJSONRejectsVisionWithWebSocket(t *testing.T) {
	profile := goldenTransportProfile("ws", "reality")
	profile.Reality = goldenRealityProfile("xtls-rprx-vision").Reality
	profile.Flow = "xtls-rprx-vision"
	_, err := BuildXrayJSON(profile, goldenOptions())
	if err == nil || !strings.Contains(err.Error(), "flow xtls-rprx-vision is only supported with tcp") {
		t.Fatalf("BuildXrayJSON() error = %v, want vision transport validation error", err)
	}
}

func TestBuildXrayJSONRealitySettingsAndDefaultFingerprint(t *testing.T) {
	profile := goldenRealityProfile("")
	profile.Reality.Fingerprint = ""
	data, err := BuildXrayJSON(profile, goldenOptions())
	if err != nil {
		t.Fatalf("BuildXrayJSON() error = %v", err)
	}
	var document struct {
		Outbounds []struct {
			StreamSettings struct {
				RealitySettings map[string]string `json:"realitySettings"`
			} `json:"streamSettings"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("unmarshal generated JSON: %v", err)
	}
	realitySettings := document.Outbounds[0].StreamSettings.RealitySettings
	want := map[string]string{
		"serverName":  "sni.example.com",
		"publicKey":   goldenRealityPublicKey,
		"shortId":     "0123456789abcdef",
		"spiderX":     "/",
		"fingerprint": "chrome",
	}
	if !reflect.DeepEqual(realitySettings, want) {
		t.Errorf("realitySettings = %#v, want %#v", realitySettings, want)
	}
}

func TestBuildXrayJSONGrpcReality(t *testing.T) {
	profile := goldenGRPCProfile("reality")
	data, err := BuildXrayJSON(profile, goldenOptions())
	if err != nil {
		t.Fatalf("BuildXrayJSON() error = %v", err)
	}
	var document struct {
		Outbounds []struct {
			StreamSettings map[string]json.RawMessage `json:"streamSettings"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("unmarshal generated JSON: %v", err)
	}
	var grpcSettings struct {
		ServiceName string `json:"serviceName"`
		MultiMode   bool   `json:"multiMode"`
	}
	if err := json.Unmarshal(document.Outbounds[0].StreamSettings["grpcSettings"], &grpcSettings); err != nil {
		t.Fatalf("unmarshal gRPC settings: %v", err)
	}
	if grpcSettings.ServiceName != "grpc-service" || !grpcSettings.MultiMode {
		t.Errorf("gRPC settings = %+v, want service=grpc-service and multiMode=true", grpcSettings)
	}
	if _, ok := document.Outbounds[0].StreamSettings["realitySettings"]; !ok {
		t.Error("REALITY settings missing for gRPC transport")
	}
}

func TestBuildXrayJSONXHTTPSettings(t *testing.T) {
	profile := goldenXHTTPProfile("tls", "packet-up")
	data, err := BuildXrayJSON(profile, goldenOptions())
	if err != nil {
		t.Fatalf("BuildXrayJSON() error = %v", err)
	}
	var document struct {
		Outbounds []struct {
			StreamSettings map[string]json.RawMessage `json:"streamSettings"`
		} `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("unmarshal generated JSON: %v", err)
	}
	var settings struct {
		Path    string            `json:"path"`
		Host    string            `json:"host"`
		Mode    string            `json:"mode"`
		Headers map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(document.Outbounds[0].StreamSettings["xhttpSettings"], &settings); err != nil {
		t.Fatalf("unmarshal XHTTP settings: %v", err)
	}
	if settings.Path != "/xhttp" || settings.Host != "cdn.example.com" ||
		settings.Mode != "packet-up" || settings.Headers["X-Test"] != "value" {
		t.Errorf("XHTTP settings = %+v", settings)
	}
	if _, exists := document.Outbounds[0].StreamSettings["tlsSettings"]; !exists {
		t.Error("TLS settings missing for XHTTP transport")
	}
}

func TestBuildXrayJSONTransportSettings(t *testing.T) {
	tests := []struct {
		name          string
		profile       config.Profile
		settingsField string
		path          string
		hostField     string
		host          string
	}{
		{
			name:          "ws",
			profile:       goldenTransportProfile("ws", "tls"),
			settingsField: "wsSettings",
			path:          "/socket",
			hostField:     "headers",
			host:          "cdn.example.com",
		},
		{
			name:          "httpupgrade",
			profile:       goldenTransportProfile("httpupgrade", "tls"),
			settingsField: "httpupgradeSettings",
			path:          "/upgrade",
			hostField:     "host",
			host:          "upgrade.example.com",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data, err := BuildXrayJSON(test.profile, goldenOptions())
			if err != nil {
				t.Fatalf("BuildXrayJSON() error = %v", err)
			}
			var document struct {
				Outbounds []struct {
					StreamSettings map[string]json.RawMessage `json:"streamSettings"`
				} `json:"outbounds"`
			}
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatalf("unmarshal generated JSON: %v", err)
			}

			var network string
			if err := json.Unmarshal(document.Outbounds[0].StreamSettings["network"], &network); err != nil {
				t.Fatalf("read network: %v", err)
			}
			if network != test.profile.Network {
				t.Fatalf("network = %q, want %q", network, test.profile.Network)
			}

			var transport map[string]json.RawMessage
			if err := json.Unmarshal(document.Outbounds[0].StreamSettings[test.settingsField], &transport); err != nil {
				t.Fatalf("read %s: %v", test.settingsField, err)
			}
			var path string
			if err := json.Unmarshal(transport["path"], &path); err != nil {
				t.Fatalf("read transport path: %v", err)
			}
			if path != test.path {
				t.Errorf("transport path = %q, want %q", path, test.path)
			}
			if test.hostField == "headers" {
				var headers map[string]string
				if err := json.Unmarshal(transport["headers"], &headers); err != nil {
					t.Fatalf("read WebSocket headers: %v", err)
				}
				if headers["Host"] != test.host {
					t.Errorf("WebSocket Host header = %q, want %q", headers["Host"], test.host)
				}
			} else {
				var host string
				if err := json.Unmarshal(transport["host"], &host); err != nil {
					t.Fatalf("read HTTPUpgrade host: %v", err)
				}
				if host != test.host {
					t.Errorf("HTTPUpgrade host = %q, want %q", host, test.host)
				}
			}
			if _, exists := document.Outbounds[0].StreamSettings["tlsSettings"]; !exists {
				t.Error("TLS settings missing")
			}
		})
	}
}

func TestBuildXrayJSONRejectsAllowInsecure(t *testing.T) {
	profile := goldenProfile("tls")
	profile.TLS.AllowInsecure = true
	if _, err := BuildXrayJSON(profile, goldenOptions()); err == nil ||
		!strings.Contains(err.Error(), "allowInsecure is not supported") {
		t.Fatalf("BuildXrayJSON() error = %v, want unsupported allowInsecure error", err)
	}
}

func tagsInbound(inbounds []struct {
	Tag string `json:"tag"`
}) []string {
	tags := make([]string, 0, len(inbounds))
	for _, inbound := range inbounds {
		tags = append(tags, inbound.Tag)
	}
	return tags
}

func tagsOutbound(outbounds []struct {
	Tag string `json:"tag"`
}) []string {
	tags := make([]string, 0, len(outbounds))
	for _, outbound := range outbounds {
		tags = append(tags, outbound.Tag)
	}
	return tags
}
