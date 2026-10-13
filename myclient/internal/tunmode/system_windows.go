//go:build windows

package tunmode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

type systemRuntime struct{}

func NewSystemRuntime() (Runtime, error) {
	return systemRuntime{}, nil
}

func (systemRuntime) IsAdmin() bool { return IsAdmin() }

func (systemRuntime) WintunAvailable() error { return CheckWintunDLL() }

func (systemRuntime) ResolveIPv4(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() == nil {
			return nil, fmt.Errorf("resolve IPv4 server address: %w", errors.New("server address is IPv6-only"))
		}
		return []net.IP{ip.To4()}, nil
	}
	addresses, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	if err != nil {
		return nil, fmt.Errorf("resolve %q using system DNS: %w", host, err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("resolve %q using system DNS: %w", host, errors.New("no IPv4 address returned"))
	}
	return addresses, nil
}

func (systemRuntime) AdapterIndex(name string) (uint32, error) {
	adapters, err := adapterAddresses()
	if err != nil {
		return 0, err
	}
	for adapter := adapters; adapter != nil; adapter = adapter.Next {
		if windows.UTF16PtrToString(adapter.FriendlyName) == name {
			return adapter.IfIndex, nil
		}
	}
	return 0, fmt.Errorf("find adapter %q: %w", name, errAdapterNotFound)
}

func (systemRuntime) DNS(ifIndex uint32) ([]string, error) {
	script := fmt.Sprintf(`$a=Get-DnsClientServerAddress -InterfaceIndex %d -AddressFamily IPv4 -ErrorAction Stop; ConvertTo-Json -Compress -InputObject @($a.ServerAddresses)`, ifIndex)
	output, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("read DNS servers for interface %d: %w: %s", ifIndex, err, strings.TrimSpace(string(output)))
	}
	var servers []string
	if err := json.Unmarshal(bytes.TrimSpace(output), &servers); err != nil {
		return nil, fmt.Errorf("decode DNS servers for interface %d: %w", ifIndex, err)
	}
	for _, server := range servers {
		if net.ParseIP(server) == nil {
			return nil, fmt.Errorf("validate DNS server %q: %w", server, errors.New("invalid IP address"))
		}
	}
	return servers, nil
}

func (systemRuntime) SetDNS(ifIndex uint32, servers []string) error {
	for _, server := range servers {
		ip := net.ParseIP(server)
		if ip == nil || ip.To4() == nil {
			return fmt.Errorf("set DNS server %q: %w", server, errors.New("expected IPv4 address"))
		}
	}
	var script string
	if len(servers) == 0 {
		script = fmt.Sprintf("Set-DnsClientServerAddress -InterfaceIndex %d -ResetServerAddresses -ErrorAction Stop", ifIndex)
	} else {
		values, _ := json.Marshal(servers)
		script = fmt.Sprintf("$s=ConvertFrom-Json -InputObject '%s'; Set-DnsClientServerAddress -InterfaceIndex %d -ServerAddresses $s -ErrorAction Stop", strings.ReplaceAll(string(values), "'", "''"), ifIndex)
	}
	output, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("set DNS servers for interface %d: %w: %s", ifIndex, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (systemRuntime) PhysicalIPv6Adapters() ([]IPv6AdapterState, error) {
	script := "$ErrorActionPreference = 'Stop'; try { " +
		"$adapters = @(); " +
		"foreach ($adapter in (Get-NetAdapter -Physical -ErrorAction Stop)) { " +
		"$binding = Get-NetAdapterBinding -Name $adapter.Name -ComponentID ms_tcpip6 -ErrorAction Stop; " +
		"if ($null -eq $binding) { throw ('IPv6 binding not found for ' + $adapter.Name) }; " +
		"$adapters += [pscustomobject]@{interface_index=[uint32]$adapter.ifIndex; name=$adapter.Name; enabled=[bool]$binding.Enabled} " +
		"}; ConvertTo-Json -Compress -InputObject @{Adapters=$adapters} " +
		"} catch { [Console]::Error.WriteLine($_.ToString()); exit 1 }"
	output, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("read physical adapter IPv6 bindings: %w: %s", err, strings.TrimSpace(string(output)))
	}
	adapters, err := decodePhysicalIPv6Adapters(output)
	if err != nil {
		return nil, fmt.Errorf("decode physical adapter IPv6 bindings: %w", err)
	}
	for _, adapter := range adapters {
		if adapter.InterfaceIndex == 0 || adapter.Name == "" {
			return nil, fmt.Errorf("validate physical adapter IPv6 binding: %w", errors.New("adapter identity is incomplete"))
		}
	}
	return adapters, nil
}

func decodePhysicalIPv6Adapters(output []byte) ([]IPv6AdapterState, error) {
	var result struct {
		Adapters json.RawMessage `json:"Adapters"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output), &result); err != nil {
		return nil, fmt.Errorf("decode adapter JSON: %w", err)
	}
	if len(result.Adapters) == 0 || string(result.Adapters) == "null" {
		return []IPv6AdapterState{}, nil
	}
	var adapters []IPv6AdapterState
	if result.Adapters[0] == '{' {
		var adapter IPv6AdapterState
		if err := json.Unmarshal(result.Adapters, &adapter); err != nil {
			return nil, fmt.Errorf("decode single adapter: %w", err)
		}
		return []IPv6AdapterState{adapter}, nil
	}
	if err := json.Unmarshal(result.Adapters, &adapters); err != nil {
		return nil, fmt.Errorf("decode adapter list: %w", err)
	}
	return adapters, nil
}

func (systemRuntime) SetIPv6Enabled(ifIndex uint32, enabled bool) error {
	action := "Disable"
	if enabled {
		action = "Enable"
	}
	script := fmt.Sprintf(
		"Get-NetAdapter -InterfaceIndex %d -ErrorAction Stop | "+
			"%s-NetAdapterBinding -ComponentID ms_tcpip6 -ErrorAction Stop | Out-Null",
		ifIndex, action,
	)
	output, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s IPv6 binding for interface %d: %w: %s", strings.ToLower(action), ifIndex, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (runtime systemRuntime) BestRoute(ip net.IP) (uint32, error) {
	route, err := bestIPForwardRoute(ip)
	if err == nil {
		return route.InterfaceIndex, nil
	}
	family := ipv4Family
	if ip.To4() == nil {
		family = ipv6Family
	}
	rows, tableErr := ipForwardRows(family)
	if tableErr != nil {
		return 0, fmt.Errorf("select route to %s: %w", ip, errors.Join(err, fmt.Errorf("read route table: %w", tableErr)))
	}
	if ifIndex, found := routeTableInterface(ip, rows); found {
		// При ошибке GetBestRoute2 таблица маршрутов подтверждает, что
		// установленный наиболее специфичный маршрут указывает на этот интерфейс.
		return ifIndex, nil
	}
	return 0, fmt.Errorf("select route to %s: GetBestRoute2 failed and route table has no unambiguous matching interface: %w", ip, err)
}

func (systemRuntime) ProcessStartTime(pid int) (uint64, error) {
	if pid < 1 {
		return 0, fmt.Errorf("read process start time: %w", errors.New("PID must be positive"))
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return 0, fmt.Errorf("open process %d for start-time query: %w", pid, err)
	}
	defer windows.CloseHandle(handle)
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return 0, fmt.Errorf("read process %d start time: %w", pid, err)
	}
	return uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime), nil
}

func (runtime systemRuntime) ProcessMatches(pid int, startTime uint64) bool {
	current, err := runtime.ProcessStartTime(pid)
	return err == nil && processIdentityMatches(pid, pid, startTime, current)
}

func adapterAddresses() (*windows.IpAdapterAddresses, error) {
	size := uint32(16 * 1024)
	for attempt := 0; attempt < 3; attempt++ {
		buffer := make([]byte, size)
		addresses := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buffer[0]))
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, windows.GAA_FLAG_INCLUDE_GATEWAYS, 0, addresses, &size)
		if err == nil {
			return addresses, nil
		}
		if !errors.Is(err, windows.ERROR_BUFFER_OVERFLOW) {
			return nil, fmt.Errorf("enumerate network adapters: %w", err)
		}
	}
	return nil, fmt.Errorf("enumerate network adapters: %w", errors.New("adapter list changed while being read"))
}
