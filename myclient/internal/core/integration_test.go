//go:build integration

package core

import (
	"context"
	"net"
	"testing"
	"time"

	"example.com/vclient/internal/config"
	dnsconfig "example.com/vclient/internal/dns"
	"example.com/vclient/internal/router"
	"example.com/vclient/internal/xrayconf"
)

func TestRunnerStartsAndStopsLocalInbound(t *testing.T) {
	socksAddress := reserveTCPAddress(t)
	httpAddress := reserveTCPAddress(t)
	profile := config.Profile{
		Name:     "integration",
		Protocol: "vless",
		Address:  "127.0.0.1",
		Port:     443,
		UUID:     "123e4567-e89b-12d3-a456-426614174000",
		Network:  "tcp",
		Security: "none",
	}
	configJSON, err := xrayconf.BuildXrayJSON(profile, xrayconf.Options{
		SOCKSListen: socksAddress,
		HTTPListen:  httpAddress,
	})
	if err != nil {
		t.Fatalf("BuildXrayJSON() error = %v", err)
	}
	runner, err := NewRunner(configJSON)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		if err := runner.Stop(); err != nil {
			t.Errorf("cleanup Runner.Stop() error = %v", err)
		}
	})

	waitForTCPListener(t, socksAddress)
	waitForTCPListener(t, httpAddress)
	if err := runner.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	restartSocksAddress := reserveTCPAddress(t)
	restartHTTPAddress := reserveTCPAddress(t)
	restartProfile := profile
	restartProfile.Security = "tls"
	restartProfile.TLS.ServerName = "proxy.example.com"
	restartJSON, err := xrayconf.BuildXrayJSON(restartProfile, xrayconf.Options{
		SOCKSListen: restartSocksAddress,
		HTTPListen:  restartHTTPAddress,
	})
	if err != nil {
		t.Fatalf("BuildXrayJSON() for restart error = %v", err)
	}
	if err := runner.Restart(context.Background(), restartJSON); err != nil {
		t.Fatalf("Restart() error = %v", err)
	}
	waitForTCPListener(t, restartSocksAddress)
	if err := runner.Stop(); err != nil {
		t.Fatalf("Stop() after restart error = %v", err)
	}
}

func TestRunnerStartsWithRoutingAndDNSConfig(t *testing.T) {
	profile := config.Profile{
		Name:     "routing-dns",
		Protocol: "vless",
		Address:  "127.0.0.1",
		Port:     443,
		UUID:     "123e4567-e89b-12d3-a456-426614174000",
		Network:  "tcp",
		Security: "none",
	}
	generated, err := xrayconf.BuildXrayJSON(profile, xrayconf.Options{
		SOCKSListen: reserveTCPAddress(t),
		HTTPListen:  reserveTCPAddress(t),
		Rules: []router.Rule{
			{Type: "field", Domain: []string{"domain:example.test"}, OutboundTag: "direct"},
		},
		DNS: &dnsconfig.Config{Servers: []dnsconfig.Server{
			{Address: "1.1.1.1:53", OutboundTag: "direct"},
			{Address: "https://dns.google/dns-query", OutboundTag: "proxy"},
		}},
	})
	if err != nil {
		t.Fatalf("BuildXrayJSON() error = %v", err)
	}
	runner, err := NewRunner(generated)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.Start(context.Background()); err != nil {
		t.Fatalf("Start() with routing and DNS config error = %v", err)
	}
	if err := runner.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestRunnerStartsWebSocketAndHTTPUpgradeTransports(t *testing.T) {
	tests := []struct {
		name        string
		network     string
		ws          config.WS
		httpUpgrade config.HTTPUpgrade
	}{
		{
			name:    "websocket",
			network: "ws",
			ws:      config.WS{Path: "/socket", Host: "cdn.example.com"},
		},
		{
			name:        "httpupgrade",
			network:     "httpupgrade",
			httpUpgrade: config.HTTPUpgrade{Path: "/upgrade", Host: "upgrade.example.com"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile := config.Profile{
				Name:        test.name,
				Protocol:    "vless",
				Address:     "127.0.0.1",
				Port:        443,
				UUID:        "123e4567-e89b-12d3-a456-426614174000",
				Network:     test.network,
				Security:    "tls",
				TLS:         config.TLS{ServerName: "proxy.example.com"},
				WS:          test.ws,
				HTTPUpgrade: test.httpUpgrade,
			}
			socksAddress := reserveTCPAddress(t)
			httpAddress := reserveTCPAddress(t)
			generated, err := xrayconf.BuildXrayJSON(profile, xrayconf.Options{
				SOCKSListen: socksAddress,
				HTTPListen:  httpAddress,
			})
			if err != nil {
				t.Fatalf("BuildXrayJSON() error = %v", err)
			}
			runner, err := NewRunner(generated)
			if err != nil {
				t.Fatalf("NewRunner() error = %v", err)
			}
			if err := runner.Start(context.Background()); err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			t.Cleanup(func() {
				if err := runner.Stop(); err != nil {
					t.Errorf("cleanup Runner.Stop() error = %v", err)
				}
			})
			waitForTCPListener(t, socksAddress)
			if err := runner.Stop(); err != nil {
				t.Fatalf("Stop() error = %v", err)
			}
		})
	}
}

func TestRunnerStartsGRPCAndXHTTPTransports(t *testing.T) {
	tests := []struct {
		name     string
		network  string
		security string
	}{
		{name: "grpc-tls", network: "grpc", security: "tls"},
		{name: "grpc-reality", network: "grpc", security: "reality"},
		{name: "xhttp-tls", network: "xhttp", security: "tls"},
		{name: "xhttp-reality", network: "xhttp", security: "reality"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile := config.Profile{
				Name:     test.name,
				Protocol: "vless",
				Address:  "127.0.0.1",
				Port:     443,
				UUID:     "123e4567-e89b-12d3-a456-426614174000",
				Network:  test.network,
				Security: test.security,
				GRPC:     config.GRPC{ServiceName: "service", MultiMode: true},
				XHTTP: config.XHTTP{
					Path:    "/xhttp",
					Host:    "cdn.example.com",
					Mode:    "packet-up",
					Headers: map[string]string{"X-Test": "value"},
				},
				TLS: config.TLS{ServerName: "proxy.example.com"},
				Reality: config.Reality{
					PublicKey:   "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
					ShortID:     "0123456789abcdef",
					ServerName:  "proxy.example.com",
					Fingerprint: "chrome",
				},
			}
			socksAddress := reserveTCPAddress(t)
			httpAddress := reserveTCPAddress(t)
			generated, err := xrayconf.BuildXrayJSON(profile, xrayconf.Options{
				SOCKSListen: socksAddress,
				HTTPListen:  httpAddress,
			})
			if err != nil {
				t.Fatalf("BuildXrayJSON() error = %v", err)
			}
			runner, err := NewRunner(generated)
			if err != nil {
				t.Fatalf("NewRunner() error = %v", err)
			}
			if err := runner.Start(context.Background()); err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			t.Cleanup(func() {
				if err := runner.Stop(); err != nil {
					t.Errorf("cleanup Runner.Stop() error = %v", err)
				}
			})
			waitForTCPListener(t, socksAddress)
			if err := runner.Stop(); err != nil {
				t.Fatalf("Stop() error = %v", err)
			}
		})
	}
}

func TestRunnerStartsExperimentalKCPTransport(t *testing.T) {
	profile := config.Profile{
		Name:     "kcp-integration",
		Protocol: "vless",
		Address:  "127.0.0.1",
		Port:     443,
		UUID:     "123e4567-e89b-12d3-a456-426614174000",
		Network:  "kcp",
		Security: "none",
		KCP:      config.KCP{MTU: 1200, TTI: 30},
	}
	socksAddress := reserveTCPAddress(t)
	httpAddress := reserveTCPAddress(t)
	generated, err := xrayconf.BuildXrayJSON(profile, xrayconf.Options{
		SOCKSListen:       socksAddress,
		HTTPListen:        httpAddress,
		AllowExperimental: true,
	})
	if err != nil {
		t.Fatalf("BuildXrayJSON() error = %v", err)
	}
	runner, err := NewRunner(generated)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() {
		if err := runner.Stop(); err != nil {
			t.Errorf("cleanup Runner.Stop() error = %v", err)
		}
	})
	waitForTCPListener(t, socksAddress)
	if err := runner.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func reserveTCPAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve local address: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release reserved local address: %v", err)
	}
	return address
}

func waitForTCPListener(t *testing.T, address string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			if closeErr := connection.Close(); closeErr != nil {
				t.Fatalf("close listener probe connection: %v", closeErr)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("Xray did not open local inbound address %s", address)
}
