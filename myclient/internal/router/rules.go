package router

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/xtls/xray-core/common/platform"
)

// Rule соответствует одному field-правилу маршрутизации Xray.
type Rule struct {
	Type        string   `json:"type"`
	Domain      []string `json:"domain,omitempty"`
	IP          []string `json:"ip,omitempty"`
	Port        string   `json:"port,omitempty"`
	Network     []string `json:"network,omitempty"`
	OutboundTag string   `json:"outboundTag,omitempty"`
	Action      string   `json:"action,omitempty"`
}

// ParseRules разбирает rules.json и проверяет синтаксис и целевые outbound-теги.
func ParseRules(body []byte, knownTags []string) ([]Rule, error) {
	var rules []Rule
	if err := json.Unmarshal(body, &rules); err != nil {
		return nil, fmt.Errorf("decode routing rules: %w", err)
	}
	if rules == nil {
		return nil, fmt.Errorf("decode routing rules: %w", errors.New("expected a JSON array"))
	}
	if err := ValidateRules(rules, knownTags); err != nil {
		return nil, fmt.Errorf("validate routing rules: %w", err)
	}
	if err := ValidateGeoAssets(rules); err != nil {
		return nil, fmt.Errorf("validate routing geodata: %w", err)
	}
	return rules, nil
}

// LoadRules загружает правила из JSON-файла.
func LoadRules(path string, knownTags []string) ([]Rule, error) {
	body, err := os.ReadFile(path) // #nosec G304 -- path is the user-selected routing rules file.
	if err != nil {
		return nil, fmt.Errorf("read routing rules %q: %w", path, err)
	}
	rules, err := ParseRules(body, knownTags)
	if err != nil {
		return nil, fmt.Errorf("load routing rules %q: %w", path, err)
	}
	return rules, nil
}

// ValidateRules проверяет условия правила и доступность его outbound.
func ValidateRules(rules []Rule, knownTags []string) error {
	tags := make(map[string]struct{}, len(knownTags))
	for _, tag := range knownTags {
		tags[tag] = struct{}{}
	}
	for index := range rules {
		rule := &rules[index]
		if rule.Type == "" {
			rule.Type = "field"
		}
		if rule.Type != "field" {
			return fmt.Errorf("rule %d has unsupported type %q", index, rule.Type)
		}
		if rule.Action != "" {
			if rule.OutboundTag != "" && rule.OutboundTag != rule.Action {
				return fmt.Errorf("rule %d action conflicts with outboundTag", index)
			}
			rule.OutboundTag = rule.Action
		}
		if rule.OutboundTag == "" {
			return fmt.Errorf("rule %d outboundTag is required", index)
		}
		if _, ok := tags[rule.OutboundTag]; !ok {
			return fmt.Errorf("rule %d references unknown outboundTag %q", index, rule.OutboundTag)
		}
		if len(rule.Domain)+len(rule.IP)+len(rule.Network) == 0 && rule.Port == "" {
			return fmt.Errorf("rule %d must contain at least one condition", index)
		}
		for _, domain := range rule.Domain {
			if err := validateDomainRule(domain); err != nil {
				return fmt.Errorf("rule %d domain %q: %w", index, domain, err)
			}
		}
		for _, ip := range rule.IP {
			if err := validateIPRule(ip); err != nil {
				return fmt.Errorf("rule %d ip %q: %w", index, ip, err)
			}
		}
		for _, network := range rule.Network {
			if network != "tcp" && network != "udp" {
				return fmt.Errorf("rule %d has unsupported network %q", index, network)
			}
		}
		if rule.Port != "" {
			if err := validatePortList(rule.Port); err != nil {
				return fmt.Errorf("rule %d port: %w", index, err)
			}
		}
	}
	return nil
}

// ValidateGeoAssets проверяет наличие баз, на которые ссылаются правила.
func ValidateGeoAssets(rules []Rule) error {
	needed := map[string]bool{}
	for _, rule := range rules {
		for _, domain := range rule.Domain {
			if strings.HasPrefix(domain, "geosite:") {
				needed["geosite.dat"] = true
			}
		}
		for _, ip := range rule.IP {
			if strings.HasPrefix(ip, "geoip:") {
				needed["geoip.dat"] = true
			}
		}
	}
	for _, file := range []string{"geoip.dat", "geosite.dat"} {
		if !needed[file] {
			continue
		}
		path := platform.GetAssetLocation(file)
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("required geodata file %q not found at %q: %w", file, path, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("required geodata file %q is not a regular file at %q", file, path)
		}
	}
	return nil
}

func validateDomainRule(rule string) error {
	if rule == "" {
		return fmt.Errorf("domain rule must not be empty")
	}
	switch {
	case strings.HasPrefix(rule, "domain:"):
		return requireRuleValue(rule, "domain:")
	case strings.HasPrefix(rule, "full:"):
		return requireRuleValue(rule, "full:")
	case strings.HasPrefix(rule, "keyword:"):
		return requireRuleValue(rule, "keyword:")
	case strings.HasPrefix(rule, "regexp:"):
		value := strings.TrimPrefix(rule, "regexp:")
		if value == "" {
			return fmt.Errorf("regular expression must not be empty")
		}
		if _, err := regexp.Compile(value); err != nil {
			return fmt.Errorf("invalid regular expression: %w", err)
		}
		return nil
	case strings.HasPrefix(rule, "geosite:"):
		return requireRuleValue(rule, "geosite:")
	default:
		return fmt.Errorf("unsupported domain rule prefix")
	}
}

func requireRuleValue(rule, prefix string) error {
	if strings.TrimSpace(strings.TrimPrefix(rule, prefix)) == "" {
		return fmt.Errorf("rule value must not be empty")
	}
	return nil
}

func validateIPRule(rule string) error {
	if strings.HasPrefix(rule, "geoip:") {
		if strings.TrimSpace(strings.TrimPrefix(rule, "geoip:")) == "" {
			return fmt.Errorf("geoip code must not be empty")
		}
		return nil
	}
	if _, _, err := net.ParseCIDR(rule); err != nil {
		if net.ParseIP(rule) == nil {
			return fmt.Errorf("invalid IP or CIDR: %w", err)
		}
	}
	return nil
}

func validatePortList(value string) error {
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return fmt.Errorf("port list contains an empty entry")
		}
		startText, endText, hasRange := strings.Cut(part, "-")
		start, err := strconv.Atoi(startText)
		if err != nil || start < 1 || start > 65535 {
			return fmt.Errorf("invalid port %q", startText)
		}
		if hasRange {
			end, err := strconv.Atoi(endText)
			if err != nil || end < start || end > 65535 {
				return fmt.Errorf("invalid port range %q", part)
			}
		}
	}
	return nil
}
