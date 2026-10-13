package tunmode

import (
	"net"
	"testing"
)

func TestBuildPlan(t *testing.T) {
	tests := []struct {
		name        string
		serverIPs   []net.IP
		allowLocal  bool
		blockIPv6   bool
		wantProtect int
		wantTunnel  int
		wantErr     bool
	}{
		{name: "IPv4 server with IPv6 blocked", serverIPs: []net.IP{net.ParseIP("203.0.113.10")}, blockIPv6: true, wantProtect: 5, wantTunnel: 2},
		{name: "multiple server IPs", serverIPs: []net.IP{net.ParseIP("203.0.113.10"), net.ParseIP("203.0.113.11")}, blockIPv6: true, wantProtect: 6, wantTunnel: 2},
		{name: "server on LAN", serverIPs: []net.IP{net.ParseIP("192.168.1.2")}, blockIPv6: true, wantProtect: 5, wantTunnel: 2},
		{name: "LAN through VPN", serverIPs: []net.IP{net.ParseIP("203.0.113.10")}, allowLocal: true, blockIPv6: true, wantProtect: 1, wantTunnel: 2},
		{name: "IPv6 blocked on physical adapters", serverIPs: []net.IP{net.ParseIP("203.0.113.10")}, blockIPv6: true, wantProtect: 5, wantTunnel: 2},
		{name: "IPv6 bypasses VPN without adapter changes", serverIPs: []net.IP{net.ParseIP("203.0.113.10")}, blockIPv6: false, wantProtect: 5, wantTunnel: 2},
		{name: "IPv6-only server unsupported", serverIPs: []net.IP{net.ParseIP("2001:db8::1")}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan, err := BuildPlan(PlanInput{
				ServerIPs: test.serverIPs, Gateway: net.ParseIP("192.0.2.1"),
				GatewayInterface: 7, TunInterface: 21,
				AllowLocalThrough: test.allowLocal, BlockIPv6: test.blockIPv6,
			})
			if (err != nil) != test.wantErr {
				t.Fatalf("BuildPlan() error = %v, wantErr %t", err, test.wantErr)
			}
			if err != nil {
				return
			}
			if len(plan.ProtectRoutes) != test.wantProtect || len(plan.AddRoutes) != test.wantTunnel {
				t.Fatalf("plan has %d protect routes and %d tunnel routes; want %d and %d",
					len(plan.ProtectRoutes), len(plan.AddRoutes), test.wantProtect, test.wantTunnel)
			}
			for _, route := range plan.AddRoutes {
				if route.Interface != 21 {
					t.Errorf("tunnel route interface = %d, want 21", route.Interface)
				}
				if route.Destination.Addr().Is6() {
					t.Errorf("IPv6 route %s should not be included in either IPv6 policy", route.Destination)
				}
			}
			for _, route := range plan.ProtectRoutes {
				if route.Interface != 7 {
					t.Errorf("protected route interface = %d, want 7", route.Interface)
				}
			}
		})
	}
}
