package dns

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestLoadAndBuildXrayDNSConfig(t *testing.T) {
	input := []byte(`{
		"servers": [
			{"address":"1.1.1.1:5353","outboundTag":"direct","domains":["full:local.example"]},
			{"address":"https://dns.google/dns-query","outboundTag":"proxy"}
		]
	}`)
	config, err := Load(input, []string{"proxy", "direct", "block"})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got, err := config.XrayConfig()
	if err != nil {
		t.Fatalf("XrayConfig() error = %v", err)
	}
	want := &XrayConfig{Servers: []XrayServer{
		{Address: "1.1.1.1", Port: 5353, Tag: "direct", Domains: []string{"full:local.example"}},
		{Address: "https://dns.google/dns-query", Tag: "proxy"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("XrayConfig() = %#v, want %#v", got, want)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if string(encoded) == "" {
		t.Fatal("marshalled Xray DNS config is empty")
	}
}

func TestLoadRejectsInvalidAddressAndOutbound(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{
			name: "invalid address",
			json: `{"servers":[{"address":"dns.example:53","outboundTag":"proxy"}]}`,
			want: "must be an IP address",
		},
		{
			name: "unknown outbound tag",
			json: `{"servers":[{"address":"1.1.1.1:53","outboundTag":"missing"}]}`,
			want: "unknown outboundTag",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Load([]byte(test.json), []string{"proxy", "direct", "block"})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Load() error = %v, want %q", err, test.want)
			}
		})
	}
}
