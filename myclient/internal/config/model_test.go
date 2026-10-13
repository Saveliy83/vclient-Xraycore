package config

import (
	"encoding/base64"
	"testing"
)

const validRealityPublicKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func TestProfileValidate(t *testing.T) {
	validProfile := Profile{
		Name:     "test",
		Protocol: "vless",
		Address:  "proxy.example.com",
		Port:     443,
		UUID:     "123e4567-e89b-12d3-a456-426614174000",
		Network:  "tcp",
		Security: "tls",
	}

	tests := []struct {
		name    string
		profile Profile
		wantErr bool
	}{
		{name: "valid TLS profile", profile: validProfile},
		{
			name: "valid Reality profile",
			profile: func() Profile {
				profile := validProfile
				profile.Security = "reality"
				profile.Reality.PublicKey = validRealityPublicKey
				return profile
			}(),
		},
		{
			name: "missing Reality public key",
			profile: func() Profile {
				profile := validProfile
				profile.Security = "reality"
				return profile
			}(),
			wantErr: true,
		},
		{
			name: "missing name",
			profile: func() Profile {
				profile := validProfile
				profile.Name = ""
				return profile
			}(),
			wantErr: true,
		},
		{
			name: "unsupported protocol",
			profile: func() Profile {
				profile := validProfile
				profile.Protocol = "vmess"
				return profile
			}(),
			wantErr: true,
		},
		{
			name: "invalid address",
			profile: func() Profile {
				profile := validProfile
				profile.Address = "bad host"
				return profile
			}(),
			wantErr: true,
		},
		{
			name: "invalid port",
			profile: func() Profile {
				profile := validProfile
				profile.Port = 65536
				return profile
			}(),
			wantErr: true,
		},
		{
			name: "invalid UUID",
			profile: func() Profile {
				profile := validProfile
				profile.UUID = "not-a-uuid"
				return profile
			}(),
			wantErr: true,
		},
		{
			name: "unsupported network",
			profile: func() Profile {
				profile := validProfile
				profile.Network = "unknown"
				return profile
			}(),
			wantErr: true,
		},
		{
			name: "valid ws path",
			profile: func() Profile {
				profile := validProfile
				profile.Network = "ws"
				profile.WS.Path = "/socket"
				return profile
			}(),
		},
		{
			name: "ws path missing slash",
			profile: func() Profile {
				profile := validProfile
				profile.Network = "ws"
				profile.WS.Path = "socket"
				return profile
			}(),
			wantErr: true,
		},
		{
			name: "ws path empty",
			profile: func() Profile {
				profile := validProfile
				profile.Network = "ws"
				return profile
			}(),
			wantErr: true,
		},
		{
			name: "valid httpupgrade path",
			profile: func() Profile {
				profile := validProfile
				profile.Network = "httpupgrade"
				profile.HTTPUpgrade.Path = "/upgrade"
				return profile
			}(),
		},
		{
			name: "httpupgrade path missing slash",
			profile: func() Profile {
				profile := validProfile
				profile.Network = "httpupgrade"
				profile.HTTPUpgrade.Path = "upgrade"
				return profile
			}(),
			wantErr: true,
		},
		{
			name: "httpupgrade path empty",
			profile: func() Profile {
				profile := validProfile
				profile.Network = "httpupgrade"
				return profile
			}(),
			wantErr: true,
		},
		{
			name: "unsupported security",
			profile: func() Profile {
				profile := validProfile
				profile.Security = "unknown"
				return profile
			}(),
			wantErr: true,
		},
		{
			name: "vision with websocket",
			profile: func() Profile {
				profile := validProfile
				profile.Network = "ws"
				profile.WS.Path = "/socket"
				profile.Flow = "xtls-rprx-vision"
				return profile
			}(),
			wantErr: true,
		},
		{
			name: "grpc requires service name",
			profile: func() Profile {
				profile := validProfile
				profile.Network = "grpc"
				return profile
			}(),
			wantErr: true,
		},
		{
			name: "grpc service name contains whitespace",
			profile: func() Profile {
				profile := validProfile
				profile.Network = "grpc"
				profile.GRPC.ServiceName = "edge service"
				return profile
			}(),
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.profile.Validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %t", err, test.wantErr)
			}
		})
	}
}

func TestProfileValidateXHTTPModeAndHeaders(t *testing.T) {
	base := Profile{
		Name:     "xhttp",
		Protocol: "vless",
		Address:  "proxy.example.com",
		Port:     443,
		UUID:     "123e4567-e89b-12d3-a456-426614174000",
		Network:  "xhttp",
		Security: "tls",
		XHTTP: XHTTP{
			Mode:    "auto",
			Headers: map[string]string{"X-Test": "value"},
		},
	}
	tests := []struct {
		name    string
		mode    string
		headers map[string]string
		wantErr bool
	}{
		{name: "empty mode defaults to auto", mode: ""},
		{name: "auto mode", mode: "auto"},
		{name: "packet-up mode", mode: "packet-up"},
		{name: "stream-up mode", mode: "stream-up"},
		{name: "stream-one mode", mode: "stream-one"},
		{name: "unsupported mode", mode: "invalid", wantErr: true},
		{name: "empty header key", mode: "auto", headers: map[string]string{"": "value"}, wantErr: true},
		{name: "whitespace header key", mode: "auto", headers: map[string]string{" \t": "value"}, wantErr: true},
		{name: "Host header must use host field", mode: "auto", headers: map[string]string{"Host": "cdn.example.com"}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile := base
			profile.XHTTP.Mode = test.mode
			if test.headers != nil {
				profile.XHTTP.Headers = test.headers
			}
			err := profile.Validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %t", err, test.wantErr)
			}
		})
	}
}

func TestProfileValidateEncryptionAndXHTTPPadding(t *testing.T) {
	mldsa65Key := base64.RawURLEncoding.EncodeToString(make([]byte, 1952))
	base := Profile{
		Name:       "encrypted",
		Protocol:   "vless",
		Address:    "proxy.example.com",
		Port:       443,
		UUID:       "123e4567-e89b-12d3-a456-426614174000",
		Network:    "xhttp",
		Security:   "reality",
		Encryption: "mlkem768x25519plus.native.0rtt.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		Reality:    Reality{PublicKey: validRealityPublicKey, MLDSA65Verify: mldsa65Key},
		XHTTP:      XHTTP{Path: "/media", Mode: "auto", XPaddingBytes: "100-1000"},
	}
	tests := []struct {
		name       string
		encryption string
		padding    string
		pqv        string
		wantErr    bool
	}{
		{name: "valid ML-KEM, ML-DSA and padding", encryption: base.Encryption, padding: "100-1000", pqv: mldsa65Key},
		{name: "unencrypted VLESS", encryption: "none"},
		{name: "unsupported encryption", encryption: "aes", wantErr: true},
		{name: "invalid encryption public key", encryption: "mlkem768x25519plus.native.0rtt.invalid", wantErr: true},
		{name: "zero padding", encryption: "none", padding: "0-100", wantErr: true},
		{name: "reversed padding range", encryption: "none", padding: "1000-100", wantErr: true},
		{name: "malformed padding range", encryption: "none", padding: "100-200-300", wantErr: true},
		{name: "invalid ML-DSA key", encryption: "none", pqv: "invalid", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile := base
			profile.Encryption = test.encryption
			profile.XHTTP.XPaddingBytes = test.padding
			profile.Reality.MLDSA65Verify = test.pqv
			if test.name == "unencrypted VLESS" {
				profile.Security = "tls"
			}
			err := profile.Validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %t", err, test.wantErr)
			}
		})
	}
}

func TestProfileValidateKCPSettings(t *testing.T) {
	base := Profile{
		Name:     "kcp",
		Protocol: "vless",
		Address:  "proxy.example.com",
		Port:     443,
		UUID:     "123e4567-e89b-12d3-a456-426614174000",
		Network:  "kcp",
		Security: "none",
	}
	tests := []struct {
		name    string
		kcp     KCP
		wantErr bool
	}{
		{name: "default settings"},
		{name: "valid mtu and tti", kcp: KCP{MTU: 1200, TTI: 30}},
		{name: "mtu too small", kcp: KCP{MTU: 20}, wantErr: true},
		{name: "tti too small", kcp: KCP{TTI: 9}, wantErr: true},
		{name: "tti too large", kcp: KCP{TTI: 1001}, wantErr: true},
		{name: "unsupported header type", kcp: KCP{HeaderType: "srtp"}, wantErr: true},
		{name: "unsupported seed", kcp: KCP{Seed: "secret"}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile := base
			profile.KCP = test.kcp
			err := profile.Validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %t", err, test.wantErr)
			}
		})
	}
}

func TestProfileValidateRealityKeyAndShortID(t *testing.T) {
	base := Profile{
		Name:     "reality",
		Protocol: "vless",
		Address:  "proxy.example.com",
		Port:     443,
		UUID:     "123e4567-e89b-12d3-a456-426614174000",
		Network:  "tcp",
		Security: "reality",
		Reality: Reality{
			PublicKey: validRealityPublicKey,
			ShortID:   "0123456789abcdef",
		},
	}

	tests := []struct {
		name      string
		publicKey string
		shortID   string
		wantErr   bool
	}{
		{name: "valid key and maximum short id", publicKey: validRealityPublicKey, shortID: "0123456789abcdef"},
		{name: "valid empty short id", publicKey: validRealityPublicKey, shortID: ""},
		{name: "short key", publicKey: "AAAA", shortID: "01", wantErr: true},
		{name: "invalid base64url key", publicKey: "!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!", shortID: "01", wantErr: true},
		{name: "padded key", publicKey: validRealityPublicKey + "=", shortID: "01", wantErr: true},
		{name: "short id odd length", publicKey: validRealityPublicKey, shortID: "abc", wantErr: true},
		{name: "short id non-hex", publicKey: validRealityPublicKey, shortID: "gh", wantErr: true},
		{name: "short id too long", publicKey: validRealityPublicKey, shortID: "0123456789abcdef0", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile := base
			profile.Reality.PublicKey = test.publicKey
			profile.Reality.ShortID = test.shortID
			err := profile.Validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %t", err, test.wantErr)
			}
		})
	}
}
