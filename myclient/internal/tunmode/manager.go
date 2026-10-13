package tunmode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"example.com/vclient/internal/config"
	"example.com/vclient/internal/core"
	"example.com/vclient/internal/xrayconf"
	"golang.org/x/net/proxy"
)

const (
	tunName        = "vclient-tun"
	defaultTunMTU  = 1500
	defaultMetric  = 1
	adapterTimeout = 15 * time.Second
	// Интервал и порог дают ядру время восстановить кратковременные сетевые сбои.
	healthCheckInterval = 10 * time.Second
	healthFailureLimit  = 3
)

var errAdapterNotFound = errors.New("interface not found")

type Runtime interface {
	IsAdmin() bool
	WintunAvailable() error
	DefaultGateway() (net.IP, uint32, error)
	ResolveIPv4(context.Context, string) ([]net.IP, error)
	AdapterIndex(string) (uint32, error)
	DNS(uint32) ([]string, error)
	SetDNS(uint32, []string) error
	PhysicalIPv6Adapters() ([]IPv6AdapterState, error)
	SetIPv6Enabled(uint32, bool) error
	AddRoute(Route) error
	DeleteRoute(Route) error
	BestRoute(net.IP) (uint32, error)
	ProcessStartTime(int) (uint64, error)
	ProcessMatches(int, uint64) bool
}

type RunnerFactory func([]byte) (core.Runner, error)

type Options struct {
	Runtime           Runtime
	RunnerFactory     RunnerFactory
	StatePath         string
	Logger            *slog.Logger
	AllowLocalThrough bool
	DNSServers        []string
	OnTunnelLost      func(error)
}

type Manager struct {
	mu            sync.Mutex
	runtime       Runtime
	factory       RunnerFactory
	statePath     string
	logger        *slog.Logger
	options       Options
	runner        core.Runner
	state         *State
	monitorCancel context.CancelFunc
	onTunnelLost  func(error)
}

func NewManager(options Options) (*Manager, error) {
	if options.Runtime == nil {
		runtime, err := NewSystemRuntime()
		if err != nil {
			return nil, fmt.Errorf("create TUN system runtime: %w", err)
		}
		options.Runtime = runtime
	}
	if options.RunnerFactory == nil {
		options.RunnerFactory = func(data []byte) (core.Runner, error) {
			runner, err := core.NewRunner(data)
			if err != nil {
				return nil, fmt.Errorf("create Xray runner: %w", err)
			}
			return runner, nil
		}
	}
	if options.StatePath == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("resolve TUN state directory: %w", err)
		}
		options.StatePath = filepath.Join(configDir, "vclient", "tun-state.json")
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if len(options.DNSServers) == 0 {
		options.DNSServers = []string{"1.1.1.1", "8.8.8.8"}
	}
	return &Manager{
		runtime: options.Runtime, factory: options.RunnerFactory, statePath: options.StatePath,
		logger: options.Logger, options: options, onTunnelLost: options.OnTunnelLost,
	}, nil
}

func (manager *Manager) Enable(ctx context.Context, profile config.Profile, socksAddr, httpAddr string, blockIPv6 bool) error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.state != nil {
		return fmt.Errorf("enable TUN: %w", errors.New("full tunnel is already enabled"))
	}
	if ctx == nil {
		return fmt.Errorf("enable TUN: %w", errors.New("context must not be nil"))
	}
	if err := profile.Validate(); err != nil {
		return fmt.Errorf("enable TUN profile: %w", err)
	}
	if !manager.runtime.IsAdmin() {
		return fmt.Errorf("enable TUN: %w", errors.New("administrator privileges are required; restart vclient as administrator"))
	}
	if err := manager.runtime.WintunAvailable(); err != nil {
		return fmt.Errorf("enable TUN: %w", err)
	}
	serverIPs, err := manager.runtime.ResolveIPv4(ctx, profile.Address)
	if err != nil {
		return fmt.Errorf("resolve VLESS server before enabling TUN: %w", err)
	}
	gateway, gatewayIf, err := manager.runtime.DefaultGateway()
	if err != nil {
		return fmt.Errorf("read original default gateway: %w", err)
	}
	processStartTime, err := manager.runtime.ProcessStartTime(os.Getpid())
	if err != nil {
		return fmt.Errorf("read vclient process start time: %w", err)
	}
	state := &State{
		PID: os.Getpid(), ProcessStartTime: processStartTime, CreatedAt: time.Now(), Gateway: gateway.String(),
		GatewayIf: gatewayIf, AddedRoutes: []Route{}, ProtectRoutes: []Route{},
	}
	if err := SaveState(manager.statePath, *state); err != nil {
		return fmt.Errorf("persist initial TUN recovery state: %w", err)
	}
	manager.state = state

	configJSON, err := xrayconf.BuildXrayJSON(profile, xrayconf.Options{
		SOCKSListen: socksAddr, HTTPListen: httpAddr, TunInbound: true,
	})
	if err != nil {
		return manager.rollback(fmt.Errorf("build Xray TUN configuration: %w", err))
	}
	runner, err := manager.factory(configJSON)
	if err != nil {
		return manager.rollback(fmt.Errorf("create Xray TUN runner: %w", err))
	}
	if err := runner.Start(ctx); err != nil {
		_ = runner.Stop()
		return manager.rollback(fmt.Errorf("start Xray TUN runner: %w", err))
	}
	manager.runner = runner
	if err := waitForAdapter(ctx, manager.runtime, tunName); err != nil {
		return manager.rollback(err)
	}
	tunIf, err := manager.runtime.AdapterIndex(tunName)
	if err != nil {
		return manager.rollback(fmt.Errorf("find Xray TUN interface: %w", err))
	}
	state.TunIf = tunIf
	if blockIPv6 {
		adapters, err := manager.runtime.PhysicalIPv6Adapters()
		if err != nil {
			return manager.rollback(fmt.Errorf("read physical adapter IPv6 state: %w", err))
		}
		state.IPv6Adapters = adapters
		if err := SaveState(manager.statePath, *state); err != nil {
			return manager.rollback(fmt.Errorf("persist physical adapter IPv6 state: %w", err))
		}
		for _, adapter := range adapters {
			if !adapter.Enabled {
				continue
			}
			if err := manager.runtime.SetIPv6Enabled(adapter.InterfaceIndex, false); err != nil {
				return manager.rollback(fmt.Errorf("disable IPv6 on physical adapter %q (interface %d): %w",
					adapter.Name, adapter.InterfaceIndex, err))
			}
		}
	}
	dns, err := manager.runtime.DNS(tunIf)
	if err != nil {
		return manager.rollback(fmt.Errorf("save original TUN DNS settings: %w", err))
	}
	state.DNS = dns
	state.DNSCaptured = true
	if err := SaveState(manager.statePath, *state); err != nil {
		return manager.rollback(fmt.Errorf("persist original TUN DNS settings: %w", err))
	}
	plan, err := BuildPlan(PlanInput{
		ServerIPs: serverIPs, Gateway: gateway, GatewayInterface: gatewayIf, TunInterface: tunIf,
		AllowLocalThrough: manager.options.AllowLocalThrough, BlockIPv6: blockIPv6,
	})
	if err != nil {
		return manager.rollback(err)
	}
	for _, route := range plan.ProtectRoutes {
		if err := manager.runtime.AddRoute(route); err != nil {
			return manager.rollback(fmt.Errorf("add protected route %s: %w", route.Destination, err))
		}
		state.ProtectRoutes = append(state.ProtectRoutes, route)
		if err := SaveState(manager.statePath, *state); err != nil {
			return manager.rollback(fmt.Errorf("persist protected route state: %w", err))
		}
	}
	for _, route := range plan.AddRoutes {
		if err := manager.runtime.AddRoute(route); err != nil {
			return manager.rollback(fmt.Errorf("add TUN route %s: %w", route.Destination, err))
		}
		state.AddedRoutes = append(state.AddedRoutes, route)
		if err := SaveState(manager.statePath, *state); err != nil {
			return manager.rollback(fmt.Errorf("persist TUN route state: %w", err))
		}
	}
	if err := manager.runtime.SetDNS(tunIf, manager.options.DNSServers); err != nil {
		return manager.rollback(fmt.Errorf("set TUN DNS servers: %w", err))
	}
	bestIf, err := manager.runtime.BestRoute(net.ParseIP("1.1.1.1").To4())
	if err != nil {
		return manager.rollback(fmt.Errorf("verify TUN route: %w", err))
	}
	if bestIf != tunIf {
		return manager.rollback(fmt.Errorf("verify TUN route: %w", fmt.Errorf("best route uses interface %d, expected %d", bestIf, tunIf)))
	}
	manager.logger.Info("full-tunnel mode enabled", "server", net.JoinHostPort(profile.Address, fmt.Sprint(profile.Port)))
	monitorCtx, cancel := context.WithCancel(ctx)
	manager.monitorCancel = cancel
	go manager.monitorTunnel(monitorCtx, socksAddr)
	return nil
}

func (manager *Manager) SetOnTunnelLost(callback func(error)) {
	manager.mu.Lock()
	manager.onTunnelLost = callback
	manager.mu.Unlock()
}

func (manager *Manager) monitorTunnel(ctx context.Context, socksAddr string) {
	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()
	var failureTracker healthFailureTracker
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			checkCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
			err := checkTunnel(checkCtx, socksAddr)
			cancel()
			disableTunnel := failureTracker.record(err)
			if err == nil {
				continue
			}
			manager.logger.Warn("full-tunnel health check failed", "attempt", failureTracker.failures, "error", err)
			if !disableTunnel {
				continue
			}
			manager.mu.Lock()
			if manager.state == nil {
				manager.mu.Unlock()
				return
			}
			disableErr := manager.disableLocked()
			callback := manager.onTunnelLost
			manager.mu.Unlock()
			if disableErr != nil {
				err = errors.Join(err, fmt.Errorf("restore direct internet after tunnel loss: %w", disableErr))
			}
			if callback != nil {
				callback(err)
			}
			return
		}
	}
}

func checkTunnel(ctx context.Context, socksAddr string) error {
	dialer, err := proxy.SOCKS5("tcp", socksAddr, nil, proxy.Direct)
	if err != nil {
		return fmt.Errorf("create TUN health-check SOCKS dialer: %w", err)
	}
	contextDialer, ok := dialer.(proxy.ContextDialer)
	if !ok {
		return fmt.Errorf("create TUN health-check SOCKS dialer: %w", errors.New("dialer does not support context"))
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return contextDialer.DialContext(ctx, network, address)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 4 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ipify.org", nil)
	if err != nil {
		return fmt.Errorf("create TUN health-check request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("request TUN health check: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("TUN health check: unexpected HTTP status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 64))
	if err != nil {
		return fmt.Errorf("read TUN health-check response: %w", err)
	}
	if net.ParseIP(strings.TrimSpace(string(body))) == nil {
		return fmt.Errorf("parse TUN health-check response: %w", errors.New("response was not an IP address"))
	}
	return nil
}

func waitForAdapter(ctx context.Context, runtime Runtime, name string) error {
	deadline := time.NewTimer(adapterTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := runtime.AdapterIndex(name); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for TUN adapter: %w", ctx.Err())
		case <-deadline.C:
			return fmt.Errorf("wait for TUN adapter: %w", errors.New("adapter did not appear within 15 seconds"))
		case <-ticker.C:
		}
	}
}

func (manager *Manager) Disable() error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.state == nil {
		if _, err := os.Stat(manager.statePath); errors.Is(err, os.ErrNotExist) {
			return nil
		}
		state, err := LoadState(manager.statePath)
		if err != nil {
			return fmt.Errorf("load TUN state for disable: %w", err)
		}
		manager.state = &state
	}
	return manager.disableLocked()
}

func (manager *Manager) rollback(cause error) error {
	if err := manager.disableLocked(); err != nil {
		return fmt.Errorf("enable TUN failed: %w", errors.Join(cause, err))
	}
	return cause
}

func (manager *Manager) disableLocked() error {
	var cleanup []error
	var remainingAdded, remainingProtect []Route
	if manager.monitorCancel != nil {
		manager.monitorCancel()
		manager.monitorCancel = nil
	}
	if manager.state != nil {
		for i := len(manager.state.AddedRoutes) - 1; i >= 0; i-- {
			if err := manager.runtime.DeleteRoute(manager.state.AddedRoutes[i]); err != nil {
				remainingAdded = append(remainingAdded, manager.state.AddedRoutes[i])
			}
		}
		for i := len(manager.state.ProtectRoutes) - 1; i >= 0; i-- {
			if err := manager.runtime.DeleteRoute(manager.state.ProtectRoutes[i]); err != nil {
				remainingProtect = append(remainingProtect, manager.state.ProtectRoutes[i])
			}
		}
		if manager.state.TunIf != 0 {
			_, adapterErr := manager.runtime.AdapterIndex(tunName)
			if errors.Is(adapterErr, errAdapterNotFound) {
				manager.logger.Warn("TUN adapter is already absent; its DNS settings no longer need restoration")
			} else {
				if adapterErr != nil {
					cleanup = append(cleanup, fmt.Errorf("find TUN adapter before settings restore: %w", adapterErr))
					manager.logger.Error("TUN adapter lookup for settings restore failed", "error", adapterErr)
				} else {
					if manager.state.DNSCaptured || manager.state.DNS != nil {
						if err := manager.runtime.SetDNS(manager.state.TunIf, manager.state.DNS); err != nil {
							cleanup = append(cleanup, fmt.Errorf("restore TUN DNS settings: %w", err))
							manager.logger.Error("TUN DNS restore failed", "error", err)
						}
					}
				}
			}
		}
		for _, adapter := range manager.state.IPv6Adapters {
			if err := manager.runtime.SetIPv6Enabled(adapter.InterfaceIndex, adapter.Enabled); err != nil {
				cleanup = append(cleanup, fmt.Errorf("restore IPv6 binding on physical adapter %q (interface %d): %w",
					adapter.Name, adapter.InterfaceIndex, err))
				manager.logger.Error("physical adapter IPv6 binding restore failed",
					"adapter", adapter.Name, "interface", adapter.InterfaceIndex, "error", err)
			}
		}
		if len(manager.state.IPv6Adapters) == 0 && manager.state.IPv6WasEnabled != nil && manager.state.TunIf != 0 {
			if err := manager.runtime.SetIPv6Enabled(manager.state.TunIf, *manager.state.IPv6WasEnabled); err != nil {
				cleanup = append(cleanup, fmt.Errorf("restore legacy TUN IPv6 binding: %w", err))
				manager.logger.Error("legacy TUN IPv6 binding restore failed", "error", err)
			}
		}
	}
	if manager.runner != nil {
		if err := manager.runner.Stop(); err != nil {
			cleanup = append(cleanup, fmt.Errorf("stop Xray TUN runner: %w", err))
			manager.logger.Error("Xray TUN runner stop failed", "error", err)
		}
		manager.runner = nil
	}
	if manager.state != nil {
		remainingAdded, addedErrors := retryRouteDeletion(manager.runtime, remainingAdded)
		remainingProtect, protectErrors := retryRouteDeletion(manager.runtime, remainingProtect)
		cleanup = append(cleanup, addedErrors...)
		cleanup = append(cleanup, protectErrors...)
		manager.state.AddedRoutes = reverseRoutes(remainingAdded)
		manager.state.ProtectRoutes = reverseRoutes(remainingProtect)
		if len(remainingAdded)+len(remainingProtect) > 0 {
			routeNames := routeDescriptions(remainingAdded, remainingProtect)
			manager.logger.Error("routes remain after TUN cleanup; run route print and remove the listed routes manually",
				"routes", routeNames)
			if err := SaveState(manager.statePath, *manager.state); err != nil {
				cleanup = append(cleanup, fmt.Errorf("persist remaining TUN routes: %w", err))
			}
			cleanup = append(cleanup, fmt.Errorf("routes remain after cleanup: %s; inspect `route print` and remove the TUN routes manually", strings.Join(routeNames, ", ")))
		}
	}
	if len(cleanup) == 0 && manager.state != nil {
		if err := os.Remove(manager.statePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			cleanup = append(cleanup, fmt.Errorf("remove TUN state: %w", err))
		} else {
			manager.state = nil
		}
	}
	if err := errors.Join(cleanup...); err != nil {
		return fmt.Errorf("disable TUN: %w", err)
	}
	manager.logger.Info("full-tunnel mode disabled")
	return nil
}

func (manager *Manager) RecoverOrphan() error {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	state, err := LoadState(manager.statePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("recover TUN state: %w", err)
	}
	if manager.runtime.ProcessMatches(state.PID, state.ProcessStartTime) {
		return nil
	}
	manager.state = &state
	manager.logger.Warn("orphaned TUN state detected; restoring routes and DNS")
	return manager.disableLocked()
}

func retryRouteDeletion(runtime Runtime, routes []Route) ([]Route, []error) {
	remaining := make([]Route, 0, len(routes))
	var failures []error
	for _, route := range routes {
		if err := runtime.DeleteRoute(route); err != nil {
			remaining = append(remaining, route)
			failures = append(failures, fmt.Errorf("delete route %s on interface %d: %w", route.Destination, route.Interface, err))
		}
	}
	return remaining, failures
}

func reverseRoutes(routes []Route) []Route {
	for left, right := 0, len(routes)-1; left < right; left, right = left+1, right-1 {
		routes[left], routes[right] = routes[right], routes[left]
	}
	return routes
}

func routeDescriptions(added, protected []Route) []string {
	descriptions := make([]string, 0, len(added)+len(protected))
	for _, route := range append(append([]Route(nil), added...), protected...) {
		gateway := "on-link"
		if route.Gateway != nil {
			gateway = route.Gateway.String()
		}
		descriptions = append(descriptions, fmt.Sprintf("%s via %s interface %d", route.Destination, gateway, route.Interface))
	}
	return descriptions
}
