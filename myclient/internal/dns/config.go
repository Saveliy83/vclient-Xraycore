package dns

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Config описывает DNS-серверы и outbound, через который отправляется DNS-трафик.
type Config struct {
	Servers []Server `json:"servers"`
}

// Server описывает классический DNS или DNS-over-HTTPS.
type Server struct {
	Address     string   `json:"address"`
	OutboundTag string   `json:"outboundTag"`
	Domains     []string `json:"domains,omitempty"`
}

// XrayConfig — JSON-форма раздела dns в конфигурации Xray.
type XrayConfig struct {
	Servers []XrayServer `json:"servers"`
}

// XrayServer соответствует NameServerConfig из infra/conf/dns.go.
type XrayServer struct {
	Address string   `json:"address"`
	Port    uint16   `json:"port,omitempty"`
	Tag     string   `json:"tag"`
	Domains []string `json:"domains,omitempty"`
}

// Load читает и проверяет конфигурацию DNS.
func Load(body []byte, knownTags []string) (*Config, error) {
	var config Config
	if err := json.Unmarshal(body, &config); err != nil {
		return nil, fmt.Errorf("decode DNS config: %w", err)
	}
	if config.Servers == nil {
		return nil, fmt.Errorf("decode DNS config: %w", fmt.Errorf("servers must be a JSON array"))
	}
	if err := config.Validate(knownTags); err != nil {
		return nil, fmt.Errorf("validate DNS config: %w", err)
	}
	return &config, nil
}

// LoadFile загружает DNS-конфигурацию из JSON-файла.
func LoadFile(path string, knownTags []string) (*Config, error) {
	body, err := os.ReadFile(path) // #nosec G304 -- path is the user-selected DNS configuration file.
	if err != nil {
		return nil, fmt.Errorf("read DNS config %q: %w", path, err)
	}
	config, err := Load(body, knownTags)
	if err != nil {
		return nil, fmt.Errorf("load DNS config %q: %w", path, err)
	}
	return config, nil
}

// Validate проверяет серверы DNS и outbound-теги.
func (config Config) Validate(knownTags []string) error {
	tags := make(map[string]struct{}, len(knownTags))
	for _, tag := range knownTags {
		tags[tag] = struct{}{}
	}
	for index, server := range config.Servers {
		if strings.TrimSpace(server.OutboundTag) == "" {
			return fmt.Errorf("DNS server %d outboundTag is required", index)
		}
		if _, ok := tags[server.OutboundTag]; !ok {
			return fmt.Errorf("DNS server %d references unknown outboundTag %q", index, server.OutboundTag)
		}
		if _, _, err := parseServerAddress(server.Address); err != nil {
			return fmt.Errorf("DNS server %d address: %w", index, err)
		}
		for _, domain := range server.Domains {
			if strings.TrimSpace(domain) == "" {
				return fmt.Errorf("DNS server %d has an empty domain rule", index)
			}
		}
	}
	return nil
}

// XrayConfig преобразует адреса серверов в схему, ожидаемую Xray-core.
func (config Config) XrayConfig() (*XrayConfig, error) {
	result := &XrayConfig{Servers: make([]XrayServer, 0, len(config.Servers))}
	for index, server := range config.Servers {
		address, port, err := parseServerAddress(server.Address)
		if err != nil {
			return nil, fmt.Errorf("DNS server %d address: %w", index, err)
		}
		result.Servers = append(result.Servers, XrayServer{
			Address: address,
			Port:    port,
			Tag:     server.OutboundTag,
			Domains: server.Domains,
		})
	}
	return result, nil
}

func parseServerAddress(value string) (string, uint16, error) {
	if strings.HasPrefix(value, "https://") {
		parsed, err := url.Parse(value)
		if err != nil {
			return "", 0, fmt.Errorf("invalid DoH URL: %w", err)
		}
		if parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
			return "", 0, fmt.Errorf("DoH address must be an https URL with a host and no user info")
		}
		if parsed.Port() != "" {
			port, err := strconv.ParseUint(parsed.Port(), 10, 16)
			if err != nil || port == 0 {
				return "", 0, fmt.Errorf("invalid DoH port %q", parsed.Port())
			}
		}
		return value, 0, nil
	}
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return "", 0, fmt.Errorf("expected IP:port or https:// DoH URL: %w", err)
	}
	if net.ParseIP(host) == nil {
		return "", 0, fmt.Errorf("DNS address host must be an IP address")
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 {
		return "", 0, fmt.Errorf("invalid DNS port %q", portText)
	}
	return host, uint16(port), nil
}
