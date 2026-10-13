package router

import (
	"strings"
	"testing"
)

func TestParseRulesConditionsAndActions(t *testing.T) {
	t.Setenv("XRAY_LOCATION_ASSET", t.TempDir())
	input := []byte(`[
		{"type":"field","domain":["domain:example.com","full:example.org","keyword:cdn","regexp:^a[.]test$"],"outboundTag":"direct"},
		{"type":"field","ip":["192.0.2.0/24","geoip:private"],"port":"80,443,1000-2000","network":["tcp","udp"],"action":"block"}
	]`)
	_, err := ParseRules(input, []string{"proxy", "direct", "block"})
	if err == nil {
		t.Fatal("ParseRules() unexpectedly accepted geodata rules without assets")
	}
	if !strings.Contains(err.Error(), "geoip.dat") {
		t.Fatalf("ParseRules() error = %v, want missing geoip.dat", err)
	}
}

func TestValidateRulesSupportedConditions(t *testing.T) {
	rules := []Rule{{
		Type:        "field",
		Domain:      []string{"domain:example.com", "full:example.org", "keyword:cdn", "regexp:^a[.]test$"},
		IP:          []string{"192.0.2.0/24", "geoip:private"},
		Port:        "80,443,1000-2000",
		Network:     []string{"tcp", "udp"},
		OutboundTag: "direct",
	}}
	if err := ValidateRules(rules, []string{"proxy", "direct", "block"}); err != nil {
		t.Fatalf("ValidateRules() error = %v", err)
	}
}

func TestValidateRulesRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name string
		rule Rule
		want string
	}{
		{
			name: "invalid CIDR",
			rule: Rule{IP: []string{"192.0.2.0/40"}, OutboundTag: "direct"},
			want: "invalid IP or CIDR",
		},
		{
			name: "unknown outbound tag",
			rule: Rule{Domain: []string{"full:example.com"}, OutboundTag: "missing"},
			want: "unknown outboundTag",
		},
		{
			name: "unknown domain prefix",
			rule: Rule{Domain: []string{"unsupported:example.com"}, OutboundTag: "direct"},
			want: "unsupported domain rule prefix",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateRules([]Rule{test.rule}, []string{"proxy", "direct", "block"})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateRules() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestParseRulesRequiresGeositeAsset(t *testing.T) {
	t.Setenv("XRAY_LOCATION_ASSET", t.TempDir())
	_, err := ParseRules([]byte(`[{"domain":["geosite:cn"],"outboundTag":"direct"}]`), []string{"proxy", "direct", "block"})
	if err == nil || !strings.Contains(err.Error(), `required geodata file "geosite.dat"`) {
		t.Fatalf("ParseRules() error = %v, want actionable missing-geosite error", err)
	}
}
