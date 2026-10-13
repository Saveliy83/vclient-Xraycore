package config

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"unicode"
)

type Profile struct {
	Name        string      `json:"name"`
	Protocol    string      `json:"protocol"`
	Address     string      `json:"address"`
	Port        int         `json:"port"`
	UUID        string      `json:"uuid"`
	Encryption  string      `json:"encryption,omitempty"`
	Flow        string      `json:"flow,omitempty"`
	Network     string      `json:"network"`
	Security    string      `json:"security"`
	TLS         TLS         `json:"tls,omitempty"`
	Reality     Reality     `json:"reality,omitempty"`
	WS          WS          `json:"ws,omitempty"`
	GRPC        GRPC        `json:"grpc,omitempty"`
	XHTTP       XHTTP       `json:"xhttp,omitempty"`
	HTTPUpgrade HTTPUpgrade `json:"http_upgrade,omitempty"`
	KCP         KCP         `json:"kcp,omitempty"`
	Warnings    []string    `json:"-"`
}

type TLS struct {
	ServerName    string   `json:"server_name,omitempty"`
	ALPN          []string `json:"alpn,omitempty"`
	Fingerprint   string   `json:"fingerprint,omitempty"`
	AllowInsecure bool     `json:"allow_insecure,omitempty"`
}

type Reality struct {
	PublicKey     string `json:"public_key,omitempty"`
	ShortID       string `json:"short_id,omitempty"`
	SpiderX       string `json:"spider_x,omitempty"`
	ServerName    string `json:"server_name,omitempty"`
	Fingerprint   string `json:"fingerprint,omitempty"`
	MLDSA65Verify string `json:"mldsa65_verify,omitempty"`
}

type WS struct {
	Path string `json:"path,omitempty"`
	Host string `json:"host,omitempty"`
}

type GRPC struct {
	ServiceName string `json:"service_name,omitempty"`
	MultiMode   bool   `json:"multi_mode,omitempty"`
}

type XHTTP struct {
	Path          string            `json:"path,omitempty"`
	Host          string            `json:"host,omitempty"`
	Mode          string            `json:"mode,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	XPaddingBytes string            `json:"x_padding_bytes,omitempty"`
}

type HTTPUpgrade struct {
	Path string `json:"path,omitempty"`
	Host string `json:"host,omitempty"`
}

type KCP struct {
	HeaderType string `json:"header_type,omitempty"`
	Seed       string `json:"seed,omitempty"`
	MTU        uint32 `json:"mtu,omitempty"`
	TTI        uint32 `json:"tti,omitempty"`
}

func (profile Profile) Validate() error {
	if strings.TrimSpace(profile.Name) == "" {
		return invalidProfile("name is required")
	}
	if profile.Protocol != "vless" {
		return invalidProfile("protocol must be vless")
	}
	if strings.TrimSpace(profile.Address) == "" {
		return invalidProfile("address is required")
	}
	if strings.ContainsAny(profile.Address, " \t\r\n") {
		return invalidProfile("address must not contain whitespace")
	}
	if profile.Port < 1 || profile.Port > 65535 {
		return invalidProfile("port must be between 1 and 65535")
	}
	if !validUUID(profile.UUID) {
		return invalidProfile("uuid must be a valid UUID")
	}

	switch profile.Network {
	case "tcp", "ws", "grpc", "xhttp", "httpupgrade", "kcp", "quic":
	default:
		return invalidProfile("network must be one of tcp, ws, grpc, xhttp, httpupgrade, kcp, quic")
	}
	switch profile.Network {
	case "ws":
		if !strings.HasPrefix(profile.WS.Path, "/") {
			return invalidProfile("ws path must start with /")
		}
	case "grpc":
		if strings.TrimSpace(profile.GRPC.ServiceName) == "" {
			return invalidProfile("grpc service_name is required")
		}
		if strings.IndexFunc(profile.GRPC.ServiceName, unicode.IsSpace) >= 0 {
			return invalidProfile("grpc service_name must not contain whitespace")
		}
	case "xhttp":
		switch profile.XHTTP.Mode {
		case "", "auto", "packet-up", "stream-up", "stream-one":
		default:
			return invalidProfile("xhttp mode must be one of auto, packet-up, stream-up, stream-one")
		}
		for key := range profile.XHTTP.Headers {
			if strings.TrimSpace(key) == "" {
				return invalidProfile("xhttp header keys must not be empty")
			}
			if strings.EqualFold(key, "host") {
				return invalidProfile("xhttp headers must not include Host; use xhttp.host")
			}
		}
		if profile.XHTTP.XPaddingBytes != "" {
			if err := validatePositiveRange(profile.XHTTP.XPaddingBytes); err != nil {
				return invalidProfile("xhttp x_padding_bytes must be a positive integer or range")
			}
		}
	case "httpupgrade":
		if !strings.HasPrefix(profile.HTTPUpgrade.Path, "/") {
			return invalidProfile("httpupgrade path must start with /")
		}
	case "kcp":
		if profile.KCP.HeaderType != "" && profile.KCP.HeaderType != "none" {
			return invalidProfile("kcp header configuration is not supported by linked xray-core version")
		}
		if profile.KCP.Seed != "" {
			return invalidProfile("kcp seed is not supported by linked xray-core version")
		}
		if profile.KCP.MTU != 0 && profile.KCP.MTU < 21 {
			return invalidProfile("kcp mtu must be at least 21")
		}
		if profile.KCP.TTI != 0 && (profile.KCP.TTI < 10 || profile.KCP.TTI > 1000) {
			return invalidProfile("kcp tti must be between 10 and 1000")
		}
	}

	if profile.Encryption != "" && profile.Encryption != "none" {
		if err := validateVLESSEncryption(profile.Encryption); err != nil {
			return invalidProfile("encryption must be none or a supported mlkem768x25519plus value")
		}
	}

	switch profile.Security {
	case "none", "tls":
	case "reality":
		if len(profile.Reality.PublicKey) != 43 {
			return invalidProfile("reality public_key must be a 43-character base64url-encoded 32-byte key")
		}
		publicKey, err := base64.RawURLEncoding.Strict().DecodeString(profile.Reality.PublicKey)
		if err != nil || len(publicKey) != 32 {
			return invalidProfile("reality public_key must be a 43-character base64url-encoded 32-byte key")
		}
		if len(profile.Reality.ShortID) > 16 {
			return invalidProfile("reality short_id must not exceed 16 hexadecimal characters")
		}
		if _, err := hex.DecodeString(profile.Reality.ShortID); err != nil {
			return invalidProfile("reality short_id must contain an even number of hexadecimal characters")
		}
		if profile.Reality.MLDSA65Verify != "" {
			verifyKey, err := base64.RawURLEncoding.Strict().DecodeString(profile.Reality.MLDSA65Verify)
			if err != nil || len(verifyKey) != 1952 {
				return invalidProfile("reality mldsa65_verify must be a base64url-encoded 1952-byte key")
			}
		}
	default:
		return invalidProfile("security must be one of none, tls, reality")
	}

	if profile.Flow == "xtls-rprx-vision" && profile.Network != "tcp" {
		return invalidProfile("flow xtls-rprx-vision is only supported with tcp")
	}

	if net.ParseIP(profile.Address) == nil && !validHostname(profile.Address) {
		return invalidProfile("address must be a valid IP address or hostname")
	}
	return nil
}

func validateVLESSEncryption(value string) error {
	parts := strings.Split(value, ".")
	if len(parts) < 4 || parts[0] != "mlkem768x25519plus" {
		return fmt.Errorf("invalid VLESS encryption format: %w", errors.New("unsupported encryption scheme"))
	}
	switch parts[1] {
	case "native", "xorpub", "random":
	default:
		return fmt.Errorf("invalid VLESS encryption mode: %w", errors.New("unsupported mode"))
	}
	switch parts[2] {
	case "0rtt", "1rtt":
	default:
		return fmt.Errorf("invalid VLESS encryption handshake: %w", errors.New("unsupported handshake"))
	}
	hasPublicKey := false
	for _, encodedKey := range parts[3:] {
		key, err := base64.RawURLEncoding.Strict().DecodeString(encodedKey)
		if err != nil || (len(key) != 32 && len(key) != 1184) {
			return fmt.Errorf("invalid VLESS encryption key: %w", errors.New("key must be base64url-encoded X25519 or ML-KEM-768 public key"))
		}
		hasPublicKey = true
	}
	if !hasPublicKey {
		return fmt.Errorf("invalid VLESS encryption parameters: %w", errors.New("at least one public key is required"))
	}
	return nil
}

func validatePositiveRange(value string) error {
	parts := strings.Split(value, "-")
	if len(parts) > 2 {
		return fmt.Errorf("invalid positive range %q: %w", value, errors.New("too many range boundaries"))
	}
	bounds := make([]int64, len(parts))
	for index, part := range parts {
		bound, err := strconv.ParseInt(part, 10, 32)
		if err != nil || bound <= 0 {
			return fmt.Errorf("invalid positive range %q: %w", value, errors.New("bounds must be positive 32-bit integers"))
		}
		bounds[index] = bound
	}
	if len(bounds) == 2 && bounds[0] > bounds[1] {
		return fmt.Errorf("invalid positive range %q: %w", value, errors.New("lower bound exceeds upper bound"))
	}
	return nil
}

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	compact := strings.ReplaceAll(value, "-", "")
	if len(compact) != 32 {
		return false
	}
	_, err := hex.DecodeString(compact)
	return err == nil
}

func validHostname(value string) bool {
	if len(value) > 253 || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') &&
				(char < '0' || char > '9') && char != '-' {
				return false
			}
		}
	}
	return true
}

func invalidProfile(message string) error {
	return fmt.Errorf("validate profile: %w", errors.New(message))
}
