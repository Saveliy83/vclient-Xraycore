package tunmode

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
)

type Route struct {
	Destination netip.Prefix `json:"destination"`
	Gateway     net.IP       `json:"gateway,omitempty"`
	Interface   uint32       `json:"interface"`
	Metric      uint32       `json:"metric"`
}

type PlanInput struct {
	ServerIPs         []net.IP
	Gateway           net.IP
	GatewayInterface  uint32
	TunInterface      uint32
	AllowLocalThrough bool
	// Политика применяется к binding физических адаптеров; IPv6-маршруты не создаются.
	BlockIPv6 bool
}

type Plan struct {
	AddRoutes     []Route
	ProtectRoutes []Route
}

var localBypassPrefixes = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("169.254.0.0/16"),
}

func BuildPlan(input PlanInput) (Plan, error) {
	if input.Gateway == nil || input.Gateway.To4() == nil {
		return Plan{}, fmt.Errorf("build TUN route plan: %w", errors.New("an IPv4 gateway is required"))
	}
	if input.GatewayInterface == 0 || input.TunInterface == 0 {
		return Plan{}, fmt.Errorf("build TUN route plan: %w", errors.New("gateway and TUN interface indexes are required"))
	}
	plan := Plan{}
	for _, serverIP := range input.ServerIPs {
		ip := serverIP.To4()
		if ip == nil {
			continue
		}
		prefix := netip.PrefixFrom(netip.AddrFrom4([4]byte{ip[0], ip[1], ip[2], ip[3]}), 32)
		plan.ProtectRoutes = append(plan.ProtectRoutes, Route{
			Destination: prefix,
			Gateway:     append(net.IP(nil), input.Gateway.To4()...),
			Interface:   input.GatewayInterface,
			Metric:      1,
		})
	}
	if len(plan.ProtectRoutes) == 0 {
		return Plan{}, fmt.Errorf("build TUN route plan: %w", errors.New("server has no IPv4 address"))
	}
	if !input.AllowLocalThrough {
		for _, prefix := range localBypassPrefixes {
			plan.ProtectRoutes = append(plan.ProtectRoutes, Route{
				Destination: prefix,
				Gateway:     append(net.IP(nil), input.Gateway.To4()...),
				Interface:   input.GatewayInterface,
				Metric:      5,
			})
		}
	}
	plan.AddRoutes = append(plan.AddRoutes,
		Route{Destination: netip.MustParsePrefix("0.0.0.0/1"), Interface: input.TunInterface, Metric: 1},
		Route{Destination: netip.MustParsePrefix("128.0.0.0/1"), Interface: input.TunInterface, Metric: 1},
	)
	return plan, nil
}
