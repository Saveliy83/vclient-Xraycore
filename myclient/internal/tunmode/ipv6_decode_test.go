package tunmode

import "testing"

func TestDecodePhysicalIPv6Adapters(t *testing.T) {
	tests := []struct {
		name string
		json string
		want int
	}{
		{
			name: "no adapters",
			json: `{"Adapters":[]}`,
			want: 0,
		},
		{
			name: "one adapter",
			json: `{"Adapters":{"interface_index":7,"name":"Ethernet","enabled":true}}`,
			want: 1,
		},
		{
			name: "multiple adapters",
			json: `{"Adapters":[{"interface_index":7,"name":"Ethernet","enabled":true},{"interface_index":8,"name":"Wi-Fi","enabled":false}]}`,
			want: 2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapters, err := decodePhysicalIPv6Adapters([]byte(test.json))
			if err != nil {
				t.Fatalf("decodePhysicalIPv6Adapters() error = %v", err)
			}
			if len(adapters) != test.want {
				t.Fatalf("decoded %d adapters, want %d", len(adapters), test.want)
			}
		})
	}
}

func TestDecodePhysicalIPv6AdaptersRejectsPowerShellErrors(t *testing.T) {
	if _, err := decodePhysicalIPv6Adapters([]byte("ForEach-Object : binding lookup failed")); err == nil {
		t.Fatal("decodePhysicalIPv6Adapters() accepted non-JSON PowerShell output")
	}
}
