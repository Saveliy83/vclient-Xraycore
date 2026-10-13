package xrayconf

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"example.com/vclient/internal/config"
	dnsconfig "example.com/vclient/internal/dns"
	"example.com/vclient/internal/router"
)

type Options struct {
	SOCKSListen       string
	HTTPListen        string
	AllowExperimental bool
	TunInbound        bool
	Rules             []router.Rule
	DNS               *dnsconfig.Config
}

type inboundConfig struct {
	Tag      string         `json:"tag"`
	Listen   string         `json:"listen"`
	Port     int            `json:"port"`
	Protocol string         `json:"protocol"`
	Settings map[string]any `json:"settings"`
}

type xrayConfig struct {
	Log       logConfig             `json:"log"`
	Inbounds  []inboundConfig       `json:"inbounds"`
	Outbounds []any                 `json:"outbounds"`
	Routing   routingConfig         `json:"routing"`
	DNS       *dnsconfig.XrayConfig `json:"dns,omitempty"`
}

type logConfig struct {
	LogLevel string `json:"loglevel"`
}

type routingConfig struct {
	Rules []routingRule `json:"rules"`
}

type routingRule struct {
	Type        string   `json:"type"`
	Network     string   `json:"network,omitempty"`
	OutboundTag string   `json:"outboundTag"`
	Domain      []string `json:"domain,omitempty"`
	IP          []string `json:"ip,omitempty"`
	Port        string   `json:"port,omitempty"`
}

// BuildXrayJSON формирует JSON-конфигурацию Xray для поддерживаемых транспортов VLESS.
func BuildXrayJSON(profile config.Profile, options Options) ([]byte, error) {
	if err := profile.Validate(); err != nil {
		return nil, fmt.Errorf("build Xray config: %w", err)
	}
	if profile.Network == "quic" {
		return nil, fmt.Errorf("build Xray config: %w", errors.New("transport quic: not supported by linked xray-core version"))
	}
	if profile.Network == "kcp" && !options.AllowExperimental {
		return nil, fmt.Errorf("build Xray config: %w", errors.New("transport kcp is experimental; enable -allow-experimental"))
	}
	switch profile.Network {
	case "tcp", "ws", "grpc", "xhttp", "httpupgrade", "kcp":
	default:
		return nil, fmt.Errorf("build Xray config: transport %s: not implemented yet", profile.Network)
	}
	if profile.Security != "none" && profile.Security != "tls" && profile.Security != "reality" {
		return nil, fmt.Errorf("build Xray config: security %s: not implemented yet", profile.Security)
	}
	if profile.Network == "kcp" && profile.Security == "reality" {
		return nil, fmt.Errorf("build Xray config: %w", errors.New("reality security with transport kcp: not supported by linked xray-core version"))
	}
	if profile.Security == "reality" && profile.Network != "tcp" && profile.Network != "grpc" && profile.Network != "xhttp" {
		return nil, fmt.Errorf("build Xray config: %w", fmt.Errorf("reality security with transport %s: not implemented yet", profile.Network))
	}
	if profile.Security == "tls" && profile.TLS.AllowInsecure {
		return nil, fmt.Errorf("build Xray config: %w", errors.New("TLS allowInsecure is not supported by this Xray-core version"))
	}
	if err := router.ValidateRules(options.Rules, []string{"proxy", "direct", "block"}); err != nil {
		return nil, fmt.Errorf("validate routing rules: %w", err)
	}
	if err := router.ValidateGeoAssets(options.Rules); err != nil {
		return nil, fmt.Errorf("validate routing geodata: %w", err)
	}
	if options.DNS != nil {
		if err := options.DNS.Validate([]string{"proxy", "direct", "block"}); err != nil {
			return nil, fmt.Errorf("validate DNS config: %w", err)
		}
	}

	socksListen, err := parseListenAddress(options.SOCKSListen)
	if err != nil {
		return nil, fmt.Errorf("build SOCKS inbound: %w", err)
	}
	httpListen, err := parseListenAddress(options.HTTPListen)
	if err != nil {
		return nil, fmt.Errorf("build HTTP inbound: %w", err)
	}

	streamSettings := map[string]any{"network": profile.Network, "security": profile.Security}
	switch profile.Network {
	case "kcp":
		kcpSettings := make(map[string]any)
		if profile.KCP.MTU != 0 {
			kcpSettings["mtu"] = profile.KCP.MTU
		}
		if profile.KCP.TTI != 0 {
			kcpSettings["tti"] = profile.KCP.TTI
		}
		streamSettings["kcpSettings"] = kcpSettings
	case "grpc":
		streamSettings["grpcSettings"] = map[string]any{
			"serviceName": profile.GRPC.ServiceName,
			"multiMode":   profile.GRPC.MultiMode,
		}
	case "xhttp":
		mode := profile.XHTTP.Mode
		if mode == "" {
			mode = "auto"
		}
		xhttpSettings := map[string]any{
			"path":    profile.XHTTP.Path,
			"mode":    mode,
			"headers": profile.XHTTP.Headers,
		}
		if profile.XHTTP.XPaddingBytes != "" {
			xhttpSettings["xPaddingBytes"] = profile.XHTTP.XPaddingBytes
		}
		if profile.XHTTP.Host != "" {
			xhttpSettings["host"] = profile.XHTTP.Host
		}
		streamSettings["xhttpSettings"] = xhttpSettings
	case "ws":
		wsSettings := map[string]any{"path": profile.WS.Path}
		if profile.WS.Host != "" {
			wsSettings["headers"] = map[string]string{"Host": profile.WS.Host}
		}
		streamSettings["wsSettings"] = wsSettings
	case "httpupgrade":
		httpUpgradeSettings := map[string]any{"path": profile.HTTPUpgrade.Path}
		if profile.HTTPUpgrade.Host != "" {
			httpUpgradeSettings["host"] = profile.HTTPUpgrade.Host
		}
		streamSettings["httpupgradeSettings"] = httpUpgradeSettings
	}
	if profile.Security == "tls" {
		serverName := profile.TLS.ServerName
		if serverName == "" {
			serverName = profile.Address
		}
		tlsSettings := map[string]any{
			"serverName":    serverName,
			"allowInsecure": profile.TLS.AllowInsecure,
		}
		if len(profile.TLS.ALPN) > 0 {
			tlsSettings["alpn"] = profile.TLS.ALPN
		}
		if profile.TLS.Fingerprint != "" {
			tlsSettings["fingerprint"] = profile.TLS.Fingerprint
		}
		streamSettings["tlsSettings"] = tlsSettings
	}
	if profile.Security == "reality" {
		serverName := profile.Reality.ServerName
		if serverName == "" {
			serverName = profile.Address
		}
		fingerprint := profile.Reality.Fingerprint
		if fingerprint == "" {
			fingerprint = "chrome"
		}
		realitySettings := map[string]any{
			"serverName":  serverName,
			"publicKey":   profile.Reality.PublicKey,
			"shortId":     profile.Reality.ShortID,
			"spiderX":     profile.Reality.SpiderX,
			"fingerprint": fingerprint,
		}
		if profile.Reality.MLDSA65Verify != "" {
			realitySettings["mldsa65Verify"] = profile.Reality.MLDSA65Verify
		}
		streamSettings["realitySettings"] = realitySettings
	}

	user := map[string]any{
		"id":         profile.UUID,
		"encryption": profile.Encryption,
	}
	if profile.Encryption == "" {
		user["encryption"] = "none"
	}
	if profile.Flow != "" {
		user["flow"] = profile.Flow
	}

	routingRules := make([]routingRule, 0, len(options.Rules)+1)
	for _, rule := range options.Rules {
		routingRules = append(routingRules, routingRule{
			Type:        rule.Type,
			OutboundTag: rule.OutboundTag,
			Domain:      rule.Domain,
			IP:          rule.IP,
			Port:        rule.Port,
			Network:     strings.Join(rule.Network, ","),
		})
	}
	routingRules = append(routingRules, routingRule{Type: "field", Network: "tcp,udp", OutboundTag: "proxy"})

	inbounds := []inboundConfig{
		{
			Tag:      "socks-in",
			Listen:   socksListen.host,
			Port:     socksListen.port,
			Protocol: "socks",
			Settings: map[string]any{"auth": "noauth", "udp": true},
		},
		{
			Tag:      "http-in",
			Listen:   httpListen.host,
			Port:     httpListen.port,
			Protocol: "http",
			Settings: map[string]any{},
		},
	}
	if options.TunInbound {
		inbounds = append(inbounds, inboundConfig{
			Protocol: "tun",
			Settings: map[string]any{"name": "vclient-tun", "MTU": 1500},
		})
	}
	generated := xrayConfig{
		Log:      logConfig{LogLevel: "warning"},
		Inbounds: inbounds,
		Outbounds: []any{
			map[string]any{
				"tag":      "proxy",
				"protocol": "vless",
				"settings": map[string]any{
					"vnext": []any{
						map[string]any{
							"address": profile.Address,
							"port":    profile.Port,
							"users":   []any{user},
						},
					},
				},
				"streamSettings": streamSettings,
			},
			map[string]any{"tag": "direct", "protocol": "freedom"},
			map[string]any{"tag": "block", "protocol": "blackhole"},
		},
		Routing: routingConfig{
			Rules: routingRules,
		},
	}
	if options.DNS != nil {
		dnsJSON, err := options.DNS.XrayConfig()
		if err != nil {
			return nil, fmt.Errorf("build DNS config: %w", err)
		}
		generated.DNS = dnsJSON
	}

	data, err := json.MarshalIndent(generated, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode Xray config: %w", err)
	}
	return append(data, '\n'), nil
}

type listenAddress struct {
	host string
	port int
}

func parseListenAddress(address string) (listenAddress, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return listenAddress{}, fmt.Errorf("invalid listen address %q: %w", address, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return listenAddress{}, fmt.Errorf("invalid listen port in %q: %w", address, err)
	}
	if port < 1 || port > 65535 {
		return listenAddress{}, fmt.Errorf("listen port in %q must be between 1 and 65535: %w", address, errors.New("invalid port"))
	}
	if host == "" {
		return listenAddress{}, fmt.Errorf("listen address %q: %w", address, errors.New("host is required"))
	}
	return listenAddress{host: host, port: port}, nil
}
