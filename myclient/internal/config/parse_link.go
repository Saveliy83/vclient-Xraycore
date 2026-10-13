package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// ParseVLESSLink разбирает ссылку VLESS и сохраняет неизвестные параметры в предупреждениях профиля.
func ParseVLESSLink(raw string) (Profile, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return Profile{}, fmt.Errorf("parse VLESS link: %w", err)
	}
	if !strings.EqualFold(parsed.Scheme, "vless") {
		return Profile{}, fmt.Errorf("parse VLESS link: %w", errors.New("scheme must be vless"))
	}
	if parsed.User == nil || parsed.User.Username() == "" || parsed.User.String() != parsed.User.Username() {
		return Profile{}, fmt.Errorf("parse VLESS link: %w", errors.New("UUID user information is required and must not contain a password"))
	}
	uuid := parsed.User.Username()
	if !validUUID(uuid) {
		return Profile{}, fmt.Errorf("parse VLESS link: %w", errors.New("invalid UUID"))
	}
	if parsed.Host == "" {
		return Profile{}, fmt.Errorf("parse VLESS link: %w", errors.New("host is required"))
	}
	if parsed.EscapedPath() != "" {
		return Profile{}, fmt.Errorf("parse VLESS link: %w", errors.New("path must be specified with the path query parameter"))
	}

	host, portText, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		return Profile{}, fmt.Errorf("parse VLESS link host and port: %w", err)
	}
	if strings.TrimSpace(host) == "" || strings.ContainsAny(host, " \t\r\n") {
		return Profile{}, fmt.Errorf("parse VLESS link: %w", errors.New("host is empty or invalid"))
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return Profile{}, fmt.Errorf("parse VLESS link port: %w", err)
	}
	if port < 1 || port > 65535 {
		return Profile{}, fmt.Errorf("parse VLESS link: %w", errors.New("port must be between 1 and 65535"))
	}

	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return Profile{}, fmt.Errorf("parse VLESS link query: %w", err)
	}
	network := strings.ToLower(query.Get("type"))
	if network == "" {
		network = "tcp"
	}
	security := strings.ToLower(query.Get("security"))
	if security == "" {
		security = "none"
	}
	if network == "mkcp" {
		network = "kcp"
	}
	encryption := query.Get("encryption")
	allowInsecure, err := parseAllowInsecure(query.Get("allowInsecure"))
	if err != nil {
		return Profile{}, fmt.Errorf("parse VLESS link allowInsecure: %w", err)
	}

	fragment, err := url.PathUnescape(parsed.EscapedFragment())
	if err != nil {
		return Profile{}, fmt.Errorf("decode VLESS link name: %w", err)
	}
	if fragment == "" {
		fragment = host
	}
	profile := Profile{
		Name:       fragment,
		Protocol:   "vless",
		Address:    host,
		Port:       port,
		UUID:       uuid,
		Encryption: encryption,
		Flow:       query.Get("flow"),
		Network:    network,
		Security:   security,
	}

	switch security {
	case "none":
	case "tls":
		profile.TLS.ServerName = query.Get("sni")
		profile.TLS.Fingerprint = query.Get("fp")
		profile.TLS.ALPN = splitALPN(query.Get("alpn"))
		profile.TLS.AllowInsecure = allowInsecure
	case "reality":
		profile.Reality = Reality{
			PublicKey:     query.Get("pbk"),
			ShortID:       query.Get("sid"),
			SpiderX:       query.Get("spx"),
			ServerName:    query.Get("sni"),
			Fingerprint:   query.Get("fp"),
			MLDSA65Verify: query.Get("pqv"),
		}
	default:
		return Profile{}, fmt.Errorf("parse VLESS link: %w", errors.New("security must be none, tls, or reality"))
	}

	switch network {
	case "tcp":
	case "ws":
		profile.WS = WS{Path: query.Get("path"), Host: query.Get("host")}
	case "grpc":
		profile.GRPC = GRPC{ServiceName: query.Get("serviceName"), MultiMode: strings.EqualFold(query.Get("mode"), "multi")}
	case "xhttp":
		extra, err := parseXHTTPExtra(query.Get("extra"))
		if err != nil {
			return Profile{}, fmt.Errorf("parse VLESS XHTTP extra: %w", err)
		}
		mode := query.Get("mode")
		if mode == "" {
			mode = extra.Mode
		}
		xPaddingBytes := query.Get("x_padding_bytes")
		if xPaddingBytes == "" {
			xPaddingBytes = extra.XPaddingBytes
		}
		profile.XHTTP = XHTTP{
			Path:          query.Get("path"),
			Host:          query.Get("host"),
			Mode:          mode,
			XPaddingBytes: xPaddingBytes,
		}
	case "httpupgrade":
		profile.HTTPUpgrade = HTTPUpgrade{Path: query.Get("path"), Host: query.Get("host")}
	case "kcp":
		mtu, err := parseOptionalUint32(query.Get("mtu"), "mtu")
		if err != nil {
			return Profile{}, fmt.Errorf("parse VLESS link KCP MTU: %w", err)
		}
		tti, err := parseOptionalUint32(query.Get("tti"), "tti")
		if err != nil {
			return Profile{}, fmt.Errorf("parse VLESS link KCP TTI: %w", err)
		}
		profile.KCP = KCP{
			HeaderType: query.Get("headerType"),
			Seed:       query.Get("seed"),
			MTU:        mtu,
			TTI:        tti,
		}
	case "quic":
	default:
		return Profile{}, fmt.Errorf("parse VLESS link: %w", errors.New("type must be tcp, ws, grpc, xhttp, httpupgrade, kcp, or quic"))
	}

	profile.Warnings = unknownQueryWarnings(query, network)
	if err := profile.Validate(); err != nil {
		return Profile{}, fmt.Errorf("validate parsed VLESS link: %w", err)
	}
	return profile, nil
}

func parseAllowInsecure(value string) (bool, error) {
	switch strings.ToLower(value) {
	case "", "0", "false":
		return false, nil
	case "1", "true":
		return true, nil
	default:
		return false, fmt.Errorf("expected 0, 1, true, or false: %w", errors.New("invalid boolean value"))
	}
}

func splitALPN(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	alpn := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			alpn = append(alpn, part)
		}
	}
	return alpn
}

type xHTTPExtra struct {
	Mode          string `json:"mode"`
	XPaddingBytes string `json:"xPaddingBytes"`
}

func parseXHTTPExtra(raw string) (xHTTPExtra, error) {
	var extra xHTTPExtra
	if strings.TrimSpace(raw) == "" {
		return extra, nil
	}
	if err := json.Unmarshal([]byte(raw), &extra); err != nil {
		return extra, fmt.Errorf("decode extra JSON: %w", err)
	}
	return extra, nil
}

func unknownQueryWarnings(query url.Values, network string) []string {
	known := map[string]struct{}{
		"type": {}, "security": {}, "sni": {}, "host": {}, "path": {},
		"serviceName": {}, "mode": {}, "flow": {}, "fp": {}, "pbk": {},
		"sid": {}, "spx": {}, "alpn": {}, "allowInsecure": {}, "encryption": {}, "pqv": {},
		"mtu": {}, "tti": {}, "headerType": {}, "seed": {},
		"x_padding_bytes": {},
	}
	if network == "xhttp" {
		known["extra"] = struct{}{}
	}
	var unknown []string
	for key := range query {
		if _, ok := known[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	warnings := make([]string, 0, len(unknown))
	for _, key := range unknown {
		warnings = append(warnings, fmt.Sprintf("unknown query parameter %q", key))
	}
	return warnings
}

func parseOptionalUint32(value, name string) (uint32, error) {
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid %s value %q: %w", name, value, err)
	}
	if parsed == 0 {
		return 0, fmt.Errorf("%s must be greater than zero: %w", name, errors.New("invalid value"))
	}
	return uint32(parsed), nil
}
