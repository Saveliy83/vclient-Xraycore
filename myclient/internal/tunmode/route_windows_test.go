//go:build windows

package tunmode

import (
	"net"
	"net/netip"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestIPHelperStructureLayout(t *testing.T) {
	var route windows.MibIpForwardRow2
	var prefix windows.IpAddressPrefix
	var address windows.RawSockaddrInet
	tests := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{name: "SOCKADDR_INET size", got: unsafe.Sizeof(address), want: 28},
		{name: "IP_ADDRESS_PREFIX size", got: unsafe.Sizeof(prefix), want: 32},
		{name: "NET_LUID size", got: unsafe.Sizeof(route.InterfaceLuid), want: 8},
		{name: "MIB_IPFORWARD_ROW2 size", got: unsafe.Sizeof(route), want: 104},
		{name: "InterfaceIndex offset", got: unsafe.Offsetof(route.InterfaceIndex), want: 8},
		{name: "DestinationPrefix offset", got: unsafe.Offsetof(route.DestinationPrefix), want: 12},
		{name: "NextHop offset", got: unsafe.Offsetof(route.NextHop), want: 44},
		{name: "ValidLifetime offset", got: unsafe.Offsetof(route.ValidLifetime), want: 76},
		{name: "Origin offset", got: unsafe.Offsetof(route.Origin), want: 100},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.got != test.want {
				t.Errorf("%s = %d, want %d", test.name, test.got, test.want)
			}
		})
	}
}

func TestSockaddrIPRoundTrip(t *testing.T) {
	tests := []string{"192.0.2.17", "2001:db8:1234:5678::abcd"}
	for _, text := range tests {
		t.Run(text, func(t *testing.T) {
			want := net.ParseIP(text)
			address, err := sockaddrFromIP(want)
			if err != nil {
				t.Fatalf("sockaddrFromIP() error = %v", err)
			}
			if got := sockaddrIP(address); !got.Equal(want) {
				t.Fatalf("sockaddr round trip = %s, want %s", got, want)
			}
		})
	}
}

func TestSelectIPv4Gateway(t *testing.T) {
	tests := []struct {
		name           string
		routeNextHop   net.IP
		adapterGateway net.IP
		want           string
	}{
		{
			name:           "route next hop preferred",
			routeNextHop:   net.ParseIP("192.0.2.1"),
			adapterGateway: net.ParseIP("192.0.2.254"),
			want:           "192.0.2.1",
		},
		{
			name:           "adapter gateway used for on-link route row",
			routeNextHop:   net.IPv4zero,
			adapterGateway: net.ParseIP("192.0.2.254"),
			want:           "192.0.2.254",
		},
		{
			name:         "invalid gateways rejected",
			routeNextHop: net.ParseIP("2001:db8::1"),
			want:         "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := selectIPv4Gateway(test.routeNextHop, test.adapterGateway)
			if got == nil {
				if test.want != "" {
					t.Fatalf("selectIPv4Gateway() = nil, want %s", test.want)
				}
				return
			}
			if got.String() != test.want {
				t.Errorf("selectIPv4Gateway() = %s, want %s", got, test.want)
			}
		})
	}
}

func TestRouteTableInterface(t *testing.T) {
	tests := []struct {
		name        string
		destination string
		routes      []Route
		want        uint32
		wantFound   bool
	}{
		{
			name:        "tunnel split default route",
			destination: "1.1.1.1",
			routes: []Route{
				{Destination: netip.MustParsePrefix("0.0.0.0/1"), Interface: 7, Metric: 1},
				{Destination: netip.MustParsePrefix("128.0.0.0/1"), Interface: 7, Metric: 1},
			},
			want:      7,
			wantFound: true,
		},
		{
			name:        "more specific physical route wins",
			destination: "1.1.1.1",
			routes: []Route{
				{Destination: netip.MustParsePrefix("0.0.0.0/1"), Interface: 7, Metric: 1},
				{Destination: netip.MustParsePrefix("1.1.1.0/24"), Interface: 9, Metric: 500},
			},
			want:      9,
			wantFound: true,
		},
		{
			name:        "equal prefix on different interfaces is ambiguous",
			destination: "1.1.1.1",
			routes: []Route{
				{Destination: netip.MustParsePrefix("0.0.0.0/1"), Interface: 7, Metric: 1},
				{Destination: netip.MustParsePrefix("0.0.0.0/1"), Interface: 9, Metric: 2},
			},
		},
		{
			name:        "no matching route",
			destination: "1.1.1.1",
			routes: []Route{
				{Destination: netip.MustParsePrefix("10.0.0.0/8"), Interface: 7, Metric: 1},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rows := make([]windows.MibIpForwardRow2, 0, len(test.routes))
			for _, route := range test.routes {
				row, err := routeToMibRow(route)
				if err != nil {
					t.Fatalf("routeToMibRow() error = %v", err)
				}
				rows = append(rows, row)
			}
			got, found := routeTableInterface(net.ParseIP(test.destination), rows)
			if found != test.wantFound || got != test.want {
				t.Errorf("routeTableInterface() = (%d, %t), want (%d, %t)", got, found, test.want, test.wantFound)
			}
		})
	}
}

func TestMibRouteConversionAndTupleMatching(t *testing.T) {
	route := Route{
		Destination: netip.MustParsePrefix("2001:db8::/32"),
		Interface:   23,
		Metric:      4,
	}
	row, err := routeToMibRow(route)
	if err != nil {
		t.Fatalf("routeToMibRow() error = %v", err)
	}
	if !routeRowMatches(row, route) {
		t.Fatal("converted MIB row does not match its source route")
	}
	route.Interface++
	if routeRowMatches(row, route) {
		t.Fatal("route tuple matched a different interface index")
	}
}
