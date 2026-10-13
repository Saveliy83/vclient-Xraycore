package app

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

type ConnState uint8

const (
	Disconnected ConnState = iota
	Connecting
	Connected
	Failed
)

func (state ConnState) String() string {
	switch state {
	case Disconnected:
		return "disconnected"
	case Connecting:
		return "connecting"
	case Connected:
		return "connected"
	case Failed:
		return "failed"
	default:
		return "unknown"
	}
}

type Snapshot struct {
	State         ConnState
	ActiveProfile string
	LastError     string
	StartedAt     time.Time
}

type RunnerFactory func(configJSON []byte) (core.Runner, error)

type TunnelManager interface {
	Enable(ctx context.Context, profile config.Profile, socksAddr, httpAddr string, blockIPv6 bool) error
	Disable() error
	SetOnTunnelLost(func(error))
}

type ServiceOptions struct {
	Profiles      *ProfileStore
	Settings      Settings
	SettingsPath  string
	RunnerFactory RunnerFactory
	Logger        *slog.Logger
	TunnelManager TunnelManager
}

type Service struct {
	profiles      *ProfileStore
	settingsPath  string
	runnerFactory RunnerFactory
	logger        *slog.Logger
	logBuffer     *logBuffer

	opMu         sync.Mutex
	mu           sync.RWMutex
	settings     Settings
	runner       core.Runner
	tunnel       TunnelManager
	tunnelActive bool
	snapshot     Snapshot
	subscribers  map[uint64]func(Snapshot)
	nextID       uint64
}

func NewService(options ServiceOptions) (*Service, error) {
	if options.Profiles == nil {
		return nil, fmt.Errorf("create app service: %w", errors.New("profile store is required"))
	}
	if options.Settings == (Settings{}) {
		options.Settings = DefaultSettings()
	}
	if err := validateSettings(options.Settings); err != nil {
		return nil, fmt.Errorf("create app service: %w", err)
	}
	if options.SettingsPath == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("resolve settings path: %w", err)
		}
		options.SettingsPath = filepath.Join(configDir, "vclient", "settings.json")
	}
	if options.RunnerFactory == nil {
		options.RunnerFactory = func(configJSON []byte) (core.Runner, error) {
			runner, err := core.NewRunner(configJSON)
			if err != nil {
				return nil, fmt.Errorf("create Xray runner: %w", err)
			}
			return runner, nil
		}
	}
	baseLogger := options.Logger
	if baseLogger == nil {
		baseLogger = slog.Default()
	}
	buffer := newLogBuffer()
	serviceLogger := slog.New(&teeHandler{next: baseLogger.Handler(), buffer: buffer})
	service := &Service{
		profiles:      options.Profiles,
		settingsPath:  options.SettingsPath,
		runnerFactory: options.RunnerFactory,
		logger:        serviceLogger,
		logBuffer:     buffer,
		settings:      options.Settings,
		tunnel:        options.TunnelManager,
		snapshot:      Snapshot{State: Disconnected},
		subscribers:   make(map[uint64]func(Snapshot)),
	}
	if service.tunnel != nil {
		service.tunnel.SetOnTunnelLost(service.handleTunnelLost)
	}
	return service, nil
}

func (service *Service) Connect(ctx context.Context, index int) error {
	service.opMu.Lock()
	defer service.opMu.Unlock()
	if ctx == nil {
		err := fmt.Errorf("connect: %w", errors.New("context must not be nil"))
		service.fail(err)
		return err
	}

	service.mu.RLock()
	runner := service.runner
	tunnelActive := service.tunnelActive
	service.mu.RUnlock()
	if runner != nil || tunnelActive {
		if err := service.disconnectLocked(); err != nil {
			return fmt.Errorf("reconnect: %w", err)
		}
	}

	service.setSnapshot(Snapshot{State: Connecting})
	profile, ok := service.profiles.Get(index)
	if !ok {
		err := fmt.Errorf("connect: %w", errors.New("profile index out of range"))
		service.fail(err)
		return err
	}
	settings := service.Settings()
	if settings.FullTunnel {
		if service.tunnel == nil {
			err := fmt.Errorf("connect in full-tunnel mode: %w", errors.New("TUN manager is unavailable on this platform"))
			service.fail(err)
			return err
		}
		if err := service.tunnel.Enable(ctx, profile, settings.SOCKSAddr, settings.HTTPAddr, settings.BlockIPv6); err != nil {
			wrapped := fmt.Errorf("enable full-tunnel mode for profile %q: %w", profile.Name, err)
			service.fail(wrapped)
			return wrapped
		}
		service.mu.Lock()
		service.tunnelActive = true
		service.mu.Unlock()
		service.setSnapshot(Snapshot{
			State: Connected, ActiveProfile: profile.Name, StartedAt: time.Now(),
		})
		service.logger.Info("full-tunnel connected", "profile", profile.Name, "server", net.JoinHostPort(profile.Address, fmt.Sprint(profile.Port)))
		return nil
	}
	configJSON, err := xrayconf.BuildXrayJSON(profile, xrayconf.Options{
		SOCKSListen: settings.SOCKSAddr,
		HTTPListen:  settings.HTTPAddr,
	})
	if err != nil {
		wrapped := fmt.Errorf("build Xray config for profile %q: %w", profile.Name, err)
		service.fail(wrapped)
		return wrapped
	}
	runner, err = service.runnerFactory(configJSON)
	if err != nil {
		wrapped := fmt.Errorf("create runner for profile %q: %w", profile.Name, err)
		service.fail(wrapped)
		return wrapped
	}
	if runner == nil {
		err := fmt.Errorf("create runner for profile %q: %w", profile.Name, errors.New("runner factory returned nil"))
		service.fail(err)
		return err
	}
	if err := runner.Start(ctx); err != nil {
		stopErr := runner.Stop()
		wrapped := fmt.Errorf("start runner for profile %q: %w", profile.Name, errors.Join(err, stopErr))
		service.fail(wrapped)
		return wrapped
	}
	service.mu.Lock()
	service.runner = runner
	service.mu.Unlock()
	service.setSnapshot(Snapshot{
		State:         Connected,
		ActiveProfile: profile.Name,
		StartedAt:     time.Now(),
	})
	service.logger.Info("connected", "profile", profile.Name, "server", net.JoinHostPort(profile.Address, fmt.Sprint(profile.Port)))
	return nil
}

func (service *Service) Disconnect() error {
	service.opMu.Lock()
	defer service.opMu.Unlock()
	return service.disconnectLocked()
}

func (service *Service) disconnectLocked() error {
	service.mu.Lock()
	runner := service.runner
	tunnelActive := service.tunnelActive
	service.mu.Unlock()
	if tunnelActive {
		if err := service.tunnel.Disable(); err != nil {
			wrapped := fmt.Errorf("disable full-tunnel mode: %w", err)
			service.fail(wrapped)
			return wrapped
		}
		service.mu.Lock()
		service.tunnelActive = false
		service.mu.Unlock()
		service.setSnapshot(Snapshot{State: Disconnected})
		service.logger.Info("full-tunnel disconnected")
		return nil
	}
	if runner == nil {
		service.setSnapshot(Snapshot{State: Disconnected})
		return nil
	}

	if err := runner.Stop(); err != nil {
		wrapped := fmt.Errorf("stop Xray runner: %w", err)
		service.fail(wrapped)
		return wrapped
	}

	service.mu.Lock()
	service.runner = nil
	service.mu.Unlock()
	service.setSnapshot(Snapshot{State: Disconnected})
	service.logger.Info("disconnected")
	return nil
}

func (service *Service) handleTunnelLost(err error) {
	service.opMu.Lock()
	defer service.opMu.Unlock()
	if err == nil {
		err = errors.New("full-tunnel connection was lost; direct internet access was restored")
	}
	service.fail(fmt.Errorf("full-tunnel connection lost: %w", err))
}

func (service *Service) State() ConnState {
	service.mu.RLock()
	defer service.mu.RUnlock()
	return service.snapshot.State
}

func (service *Service) Snapshot() Snapshot {
	service.mu.RLock()
	defer service.mu.RUnlock()
	return service.snapshot
}

func (service *Service) Subscribe(subscriber func(Snapshot)) func() {
	if subscriber == nil {
		return func() {}
	}
	service.mu.Lock()
	id := service.nextID
	service.nextID++
	service.subscribers[id] = subscriber
	service.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			service.mu.Lock()
			delete(service.subscribers, id)
			service.mu.Unlock()
		})
	}
}

func (service *Service) Logs(minLevel string) []LogEntry {
	level, err := parseLogLevel(minLevel)
	if err != nil {
		return nil
	}
	return service.logBuffer.list(level)
}

func (service *Service) SubscribeLogs(subscriber func(LogEntry)) func() {
	return service.logBuffer.subscribe(subscriber)
}

func (service *Service) ClearLogs() {
	service.logBuffer.clear()
}

func (service *Service) Logger() *slog.Logger {
	return service.logger
}

func (service *Service) Settings() Settings {
	service.mu.RLock()
	defer service.mu.RUnlock()
	return service.settings
}

func (service *Service) Profiles() *ProfileStore {
	return service.profiles
}

func (service *Service) UpdateSettings(settings Settings) error {
	if err := validateSettings(settings); err != nil {
		return fmt.Errorf("update app settings: %w", err)
	}
	service.mu.Lock()
	service.settings = settings
	service.mu.Unlock()
	return nil
}

func (service *Service) CheckExternalIP(ctx context.Context) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("check external IP: %w", errors.New("context must not be nil"))
	}
	requestContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	settings := service.Settings()
	dialer, err := proxy.SOCKS5("tcp", settings.SOCKSAddr, nil, proxy.Direct)
	if err != nil {
		return "", fmt.Errorf("create SOCKS dialer: %w", err)
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if contextDialer, ok := dialer.(proxy.ContextDialer); ok {
				return contextDialer.DialContext(ctx, network, address)
			}
			return nil, fmt.Errorf("dial through SOCKS proxy: %w", errors.New("SOCKS dialer does not support context"))
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, "https://api.ipify.org", nil)
	if err != nil {
		return "", fmt.Errorf("create external IP request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("request external IP through SOCKS proxy: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		if err := response.Body.Close(); err != nil {
			return "", fmt.Errorf("close external IP response: %w", err)
		}
		return "", fmt.Errorf("request external IP: %w", fmt.Errorf("unexpected HTTP status %d", response.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 64))
	closeErr := response.Body.Close()
	if err != nil {
		return "", fmt.Errorf("read external IP response: %w", err)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close external IP response: %w", closeErr)
	}
	address := strings.TrimSpace(string(body))
	if net.ParseIP(address) == nil {
		return "", fmt.Errorf("parse external IP response: %w", errors.New("response is not an IP address"))
	}
	return address, nil
}

func (service *Service) Shutdown() error {
	disconnectErr := service.Disconnect()
	profileErr := service.profiles.Save()
	settingsErr := SaveSettingsTo(service.settingsPath, service.Settings())
	if err := errors.Join(disconnectErr, profileErr, settingsErr); err != nil {
		return fmt.Errorf("shutdown app service: %w", err)
	}
	return nil
}

func (service *Service) fail(err error) {
	service.setSnapshot(Snapshot{State: Failed, LastError: err.Error()})
	service.logger.Error("operation failed")
}

func (service *Service) setSnapshot(snapshot Snapshot) {
	service.mu.Lock()
	if service.snapshot == snapshot {
		service.mu.Unlock()
		return
	}
	service.snapshot = snapshot
	subscribers := make([]func(Snapshot), 0, len(service.subscribers))
	for _, subscriber := range service.subscribers {
		subscribers = append(subscribers, subscriber)
	}
	service.mu.Unlock()
	for _, subscriber := range subscribers {
		subscriber(snapshot)
	}
}
