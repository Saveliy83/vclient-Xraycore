//go:build windows

package tunmode

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	ipv4Family = uint16(windows.AF_INET)
	ipv6Family = uint16(windows.AF_INET6)
)

var (
	iphlpapi                  = syscall.NewLazyDLL("iphlpapi.dll")
	procCreateIpForwardEntry2 = iphlpapi.NewProc("CreateIpForwardEntry2")
	procDeleteIpForwardEntry2 = iphlpapi.NewProc("DeleteIpForwardEntry2")
	procGetBestRoute2         = iphlpapi.NewProc("GetBestRoute2")
)

// ABI вызовов и поля MIB_IPFORWARD_ROW2 сверены с документацией Microsoft:
// https://learn.microsoft.com/en-us/windows/win32/api/netioapi/ns-netioapi-mib_ipforward_row2
// https://learn.microsoft.com/en-us/windows/win32/api/netioapi/nf-netioapi-createipforwardentry2
// https://learn.microsoft.com/en-us/windows/win32/api/netioapi/nf-netioapi-deleteipforwardentry2
// https://learn.microsoft.com/en-us/windows/win32/api/netioapi/nf-netioapi-getbestroute2
// https://learn.microsoft.com/en-us/windows/win32/api/ws2ipdef/ns-ws2ipdef-sockaddr_inet
// https://learn.microsoft.com/en-us/windows/win32/api/ifdef/ns-ifdef-net_luid_lh
// x/sys/windows v0.42.0 предоставляет типы и таблицу маршрутов, но не обёртки
// этих функций; указатели передаются по ABI netioapi.h. Размеры и смещения типов
// x/sys дополнительно закреплены Windows-тестом layout.
func (systemRuntime) AddRoute(route Route) error {
	row, err := routeToMibRow(route)
	if err != nil {
		return fmt.Errorf("prepare route %s: %w", route.Destination, err)
	}
	if err := procCreateIpForwardEntry2.Find(); err != nil {
		return fmt.Errorf("load CreateIpForwardEntry2: %w", err)
	}
	result, _, _ := procCreateIpForwardEntry2.Call(uintptr(unsafe.Pointer(&row)))
	if result != 0 {
		return fmt.Errorf("CreateIpForwardEntry2 for %s: %w", route.Destination, syscall.Errno(result))
	}
	return nil
}

func (systemRuntime) DeleteRoute(route Route) error {
	if !route.Destination.IsValid() || route.Interface == 0 {
		return fmt.Errorf("delete route: %w", errors.New("valid destination and interface are required"))
	}
	family := ipv4Family
	if route.Destination.Addr().Is6() {
		family = ipv6Family
	}
	rows, err := ipForwardRows(family)
	if err != nil {
		return fmt.Errorf("enumerate routes before deleting %s: %w", route.Destination, err)
	}
	var cleanup []error
	found := false
	if err := procDeleteIpForwardEntry2.Find(); err != nil {
		return fmt.Errorf("load DeleteIpForwardEntry2: %w", err)
	}
	for _, row := range rows {
		if !routeRowMatches(row, route) {
			continue
		}
		found = true
		result, _, _ := procDeleteIpForwardEntry2.Call(uintptr(unsafe.Pointer(&row)))
		if result != 0 {
			cleanup = append(cleanup, fmt.Errorf("DeleteIpForwardEntry2 for %s on interface %d: %w",
				route.Destination, route.Interface, syscall.Errno(result)))
		}
	}
	if !found {
		return nil
	}
	if err := errors.Join(cleanup...); err != nil {
		return fmt.Errorf("delete matching route %s: %w", route.Destination, err)
	}
	return nil
}

func (systemRuntime) DefaultGateway() (net.IP, uint32, error) {
	bestRoute, err := bestIPForwardRoute(net.ParseIP("1.1.1.1").To4())
	if err != nil {
		return nil, 0, fmt.Errorf("find current IPv4 default route: %w", err)
	}
	adapters, err := adapterAddresses()
	if err != nil {
		return nil, 0, fmt.Errorf("enumerate adapters for IPv4 gateway: %w", err)
	}
	rows, err := ipForwardRows(ipv4Family)
	if err != nil {
		return nil, 0, fmt.Errorf("enumerate IPv4 default routes: %w", err)
	}
	var selected *windows.MibIpForwardRow2
	var fallback *windows.MibIpForwardRow2
	for index := range rows {
		prefix, ok := prefixFromMib(rows[index].DestinationPrefix)
		if !ok || prefix != netip.MustParsePrefix("0.0.0.0/0") {
			continue
		}
		if !hasIPv4Gateway(sockaddrIP(rows[index].NextHop)) &&
			!hasIPv4Gateway(adapterIPv4Gateway(adapters, rows[index].InterfaceIndex)) {
			continue
		}
		if fallback == nil || rows[index].Metric < fallback.Metric {
			fallback = &rows[index]
		}
		if rows[index].InterfaceIndex == bestRoute.InterfaceIndex &&
			(selected == nil || rows[index].Metric < selected.Metric) {
			selected = &rows[index]
		}
	}
	if selected == nil {
		selected = fallback
	}
	if selected != nil {
		gateway := selectIPv4Gateway(sockaddrIP(selected.NextHop),
			adapterIPv4Gateway(adapters, selected.InterfaceIndex))
		if hasIPv4Gateway(gateway) {
			return gateway.To4(), selected.InterfaceIndex, nil
		}
	}
	// GetBestRoute2 can still identify the effective gateway when the matching
	// route-table row is represented as on-link and has an unspecified next hop.
	gateway := sockaddrIP(bestRoute.NextHop)
	if hasIPv4Gateway(gateway) {
		return gateway.To4(), bestRoute.InterfaceIndex, nil
	}
	return nil, 0, fmt.Errorf("read current IPv4 default gateway: %w", errors.New("no IPv4 default route with a usable gateway was found"))
}

func hasIPv4Gateway(gateway net.IP) bool {
	ipv4 := gateway.To4()
	return ipv4 != nil && !ipv4.IsUnspecified()
}

func selectIPv4Gateway(routeNextHop, adapterGateway net.IP) net.IP {
	if hasIPv4Gateway(routeNextHop) {
		return routeNextHop.To4()
	}
	if hasIPv4Gateway(adapterGateway) {
		return adapterGateway.To4()
	}
	return nil
}

func adapterIPv4Gateway(adapters *windows.IpAdapterAddresses, ifIndex uint32) net.IP {
	for adapter := adapters; adapter != nil; adapter = adapter.Next {
		if adapter.IfIndex != ifIndex {
			continue
		}
		for gateway := adapter.FirstGatewayAddress; gateway != nil; gateway = gateway.Next {
			ip := gateway.Address.IP()
			if hasIPv4Gateway(ip) {
				return append(net.IP(nil), ip.To4()...)
			}
		}
	}
	return nil
}

func bestIPForwardRoute(destination net.IP) (windows.MibIpForwardRow2, error) {
	dst, err := sockaddrFromIP(destination)
	if err != nil {
		return windows.MibIpForwardRow2{}, fmt.Errorf("encode route destination: %w", err)
	}
	var route windows.MibIpForwardRow2
	var source windows.RawSockaddrInet
	if err := procGetBestRoute2.Find(); err != nil {
		return windows.MibIpForwardRow2{}, fmt.Errorf("load GetBestRoute2: %w", err)
	}
	result, _, _ := procGetBestRoute2.Call(
		0, 0, 0, uintptr(unsafe.Pointer(&dst)), 0,
		uintptr(unsafe.Pointer(&route)), uintptr(unsafe.Pointer(&source)),
	)
	if result != 0 {
		return windows.MibIpForwardRow2{}, fmt.Errorf("GetBestRoute2 for %s: %w", destination, syscall.Errno(result))
	}
	return route, nil
}

func routeTableInterface(destination net.IP, rows []windows.MibIpForwardRow2) (uint32, bool) {
	ip, ok := netip.AddrFromSlice(destination)
	if !ok {
		return 0, false
	}
	ip = ip.Unmap()
	var bestPrefixLength int = -1
	var bestInterface uint32
	ambiguous := false
	for _, row := range rows {
		prefix, ok := prefixFromMib(row.DestinationPrefix)
		if !ok || prefix.Addr().Is4() != ip.Is4() || !prefix.Contains(ip) {
			continue
		}
		if prefix.Bits() > bestPrefixLength {
			bestPrefixLength = prefix.Bits()
			bestInterface = row.InterfaceIndex
			ambiguous = false
			continue
		}
		if prefix.Bits() == bestPrefixLength && row.InterfaceIndex != bestInterface {
			ambiguous = true
		}
	}
	if bestPrefixLength < 0 || ambiguous || bestInterface == 0 {
		return 0, false
	}
	return bestInterface, true
}

func ipForwardRows(family uint16) ([]windows.MibIpForwardRow2, error) {
	var table *windows.MibIpForwardTable2
	if err := windows.GetIpForwardTable2(family, &table); err != nil {
		return nil, fmt.Errorf("GetIpForwardTable2: %w", err)
	}
	if table == nil {
		return nil, errors.New("GetIpForwardTable2 returned a nil route table")
	}
	defer windows.FreeMibTable(unsafe.Pointer(table))
	return append([]windows.MibIpForwardRow2(nil), table.Rows()...), nil
}

func routeToMibRow(route Route) (windows.MibIpForwardRow2, error) {
	if !route.Destination.IsValid() || route.Interface == 0 {
		return windows.MibIpForwardRow2{}, errors.New("valid destination and interface are required")
	}
	prefix := route.Destination.Masked()
	prefixAddress, err := sockaddrFromIP(net.IP(prefix.Addr().AsSlice()))
	if err != nil {
		return windows.MibIpForwardRow2{}, fmt.Errorf("encode destination prefix: %w", err)
	}
	nextHopIP := route.Gateway
	if nextHopIP == nil {
		if prefix.Addr().Is4() {
			nextHopIP = net.IPv4zero
		} else {
			nextHopIP = net.IPv6zero
		}
	}
	nextHop, err := sockaddrFromIP(nextHopIP)
	if err != nil {
		return windows.MibIpForwardRow2{}, fmt.Errorf("encode route next hop: %w", err)
	}
	if prefix.Addr().Is4() != (nextHop.Family == ipv4Family) {
		return windows.MibIpForwardRow2{}, errors.New("next-hop address family differs from destination")
	}
	return windows.MibIpForwardRow2{
		InterfaceIndex:    route.Interface,
		DestinationPrefix: windows.IpAddressPrefix{Prefix: prefixAddress, PrefixLength: uint8(prefix.Bits())},
		NextHop:           nextHop,
		ValidLifetime:     ^uint32(0),
		PreferredLifetime: ^uint32(0),
		Metric:            route.Metric,
		Protocol:          windows.MIB_IPPROTO_NETMGMT,
	}, nil
}

func routeRowMatches(row windows.MibIpForwardRow2, route Route) bool {
	destination, ok := prefixFromMib(row.DestinationPrefix)
	if !ok || destination != route.Destination.Masked() || row.InterfaceIndex != route.Interface {
		return false
	}
	expectedHop := route.Gateway
	if expectedHop == nil {
		if route.Destination.Addr().Is4() {
			expectedHop = net.IPv4zero
		} else {
			expectedHop = net.IPv6zero
		}
	}
	return sockaddrIP(row.NextHop).Equal(expectedHop)
}

func prefixFromMib(prefix windows.IpAddressPrefix) (netip.Prefix, bool) {
	ip := sockaddrIP(prefix.Prefix)
	if ip == nil {
		return netip.Prefix{}, false
	}
	address, ok := netip.AddrFromSlice(ip)
	if !ok || int(prefix.PrefixLength) > address.BitLen() {
		return netip.Prefix{}, false
	}
	return netip.PrefixFrom(address, int(prefix.PrefixLength)).Masked(), true
}

func sockaddrFromIP(ip net.IP) (windows.RawSockaddrInet, error) {
	var address windows.RawSockaddrInet
	if ipv4 := ip.To4(); ipv4 != nil {
		address.Family = ipv4Family
		address.Data[0] = binary.LittleEndian.Uint32(ipv4)
		return address, nil
	}
	if ipv6 := ip.To16(); ipv6 != nil {
		address.Family = ipv6Family
		for index := range 4 {
			address.Data[index+1] = binary.LittleEndian.Uint32(ipv6[index*4 : index*4+4])
		}
		return address, nil
	}
	return windows.RawSockaddrInet{}, errors.New("invalid IP address")
}

func sockaddrIP(address windows.RawSockaddrInet) net.IP {
	switch address.Family {
	case ipv4Family:
		ip := make(net.IP, net.IPv4len)
		binary.LittleEndian.PutUint32(ip, address.Data[0])
		return ip
	case ipv6Family:
		ip := make(net.IP, net.IPv6len)
		for index := range 4 {
			binary.LittleEndian.PutUint32(ip[index*4:index*4+4], address.Data[index+1])
		}
		return ip
	default:
		return nil
	}
}
