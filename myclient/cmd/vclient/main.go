package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"example.com/vclient/internal/config"
	xrayrunner "example.com/vclient/internal/core"
	dnsconfig "example.com/vclient/internal/dns"
	"example.com/vclient/internal/logger"
	"example.com/vclient/internal/reload"
	"example.com/vclient/internal/router"
	"example.com/vclient/internal/systemproxy"
	"example.com/vclient/internal/xrayconf"
)

var version = "dev"

const shutdownTimeout = 5 * time.Second

type options struct {
	configPath        string
	link              string
	listenSOCKS       string
	listenHTTP        string
	logLevel          string
	logFile           string
	rulesPath         string
	dnsPath           string
	subURL            string
	subInterval       time.Duration
	profileName       string
	reloadToken       string
	systemProxy       bool
	allowExperimental bool
	showVersion       bool
}

func parseFlags(args []string) (options, error) {
	opts := options{}
	flags := flag.NewFlagSet("vclient", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&opts.configPath, "config", "", "path to JSON config")
	flags.StringVar(&opts.link, "link", "", "VLESS link for quick start")
	flags.StringVar(&opts.listenSOCKS, "listen-socks", "127.0.0.1:1080", "SOCKS listen address")
	flags.StringVar(&opts.listenHTTP, "listen-http", "127.0.0.1:1081", "HTTP listen address")
	flags.StringVar(&opts.logLevel, "log-level", "info", "log level: debug, info, warn, error")
	flags.StringVar(&opts.logFile, "log-file", "", "optional log file path")
	flags.StringVar(&opts.rulesPath, "rules", "", "optional routing rules JSON file")
	flags.StringVar(&opts.dnsPath, "dns", "", "optional DNS settings JSON file")
	flags.StringVar(&opts.subURL, "sub-url", "", "subscription URL")
	flags.DurationVar(&opts.subInterval, "sub-interval", 6*time.Hour, "subscription refresh interval")
	flags.StringVar(&opts.profileName, "profile", "", "profile name in subscription")
	flags.StringVar(&opts.reloadToken, "reload-token", "", "token for the local Windows reload endpoint")
	flags.BoolVar(&opts.systemProxy, "system-proxy", false, "set the Windows system proxy")
	flags.BoolVar(&opts.allowExperimental, "allow-experimental", false, "allow experimental transports")
	flags.BoolVar(&opts.showVersion, "version", false, "print version and exit")
	if err := flags.Parse(args); err != nil {
		return options{}, fmt.Errorf("parse command-line flags: %w", err)
	}
	if flags.NArg() != 0 {
		return options{}, fmt.Errorf("parse command-line flags: %w", errors.New("unexpected arguments: "+strings.Join(flags.Args(), " ")))
	}
	if opts.configPath != "" && opts.link != "" {
		return options{}, fmt.Errorf("parse command-line flags: %w", errors.New("-config and -link cannot be used together"))
	}
	if opts.subURL != "" && (opts.configPath != "" || opts.link != "") {
		return options{}, fmt.Errorf("parse command-line flags: %w", errors.New("-sub-url cannot be combined with -config or -link"))
	}
	if opts.subURL == "" && opts.profileName != "" {
		return options{}, fmt.Errorf("parse command-line flags: %w", errors.New("-profile requires -sub-url"))
	}
	if opts.subURL != "" && opts.subInterval <= 0 {
		return options{}, fmt.Errorf("parse command-line flags: %w", errors.New("-sub-interval must be greater than zero"))
	}
	if _, err := logger.ParseLevel(opts.logLevel); err != nil {
		return options{}, fmt.Errorf("parse command-line flags: %w", err)
	}
	return opts, nil
}

func main() {
	if err := execute(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func execute(args []string) (returnErr error) {
	var appLogger *slog.Logger
	var logCloser io.Closer
	var runner *xrayrunner.XrayRunner
	var endpointShutdown func(context.Context) error
	var restoreProxy func() error
	var runContext context.Context
	var cancelRun context.CancelFunc

	defer func() {
		if recovered := recover(); recovered != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("application panic: %w", errors.New(fmt.Sprint(recovered))))
		}
		if cancelRun != nil {
			cancelRun()
		}
		if endpointShutdown != nil {
			ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			err := endpointShutdown(ctx)
			cancel()
			if err != nil {
				returnErr = errors.Join(returnErr, err)
			}
		}
		if runner != nil {
			if err := stopRunnerWithinTimeout(runner); err != nil {
				returnErr = errors.Join(returnErr, fmt.Errorf("stop Xray during cleanup: %w", err))
			}
		}
		if restoreProxy != nil {
			if err := restoreProxy(); err != nil {
				returnErr = errors.Join(returnErr, err)
			}
		}
		if logCloser != nil {
			if err := logCloser.Close(); err != nil {
				returnErr = errors.Join(returnErr, fmt.Errorf("close logger: %w", err))
			}
		}
	}()

	opts, err := parseFlags(args)
	if err != nil {
		return err
	}
	if opts.showVersion {
		if _, err := fmt.Fprintln(os.Stdout, version); err != nil {
			return fmt.Errorf("write version: %w", err)
		}
		return nil
	}

	appLogger, logCloser, err = logger.New(opts.logLevel, opts.logFile)
	if err != nil {
		return fmt.Errorf("initialize logger: %w", err)
	}
	var startupProfile config.Profile
	prepare := func(ctx context.Context) ([]byte, error) {
		configJSON, profile, prepareErr := prepareRuntimeConfig(ctx, opts, appLogger)
		if prepareErr != nil {
			return nil, prepareErr
		}
		startupProfile = profile
		for _, warning := range profile.Warnings {
			appLogger.Warn("profile contains an unsupported link parameter", slog.String("warning", warning))
		}
		logExperimentalTransport(appLogger, profile, opts)
		return configJSON, nil
	}
	xrayJSON, err := prepare(context.Background())
	if err != nil {
		return fmt.Errorf("prepare Xray config: %w", err)
	}
	runner, err = xrayrunner.NewRunner(xrayJSON)
	if err != nil {
		return fmt.Errorf("create Xray runner: %w", err)
	}

	signalContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	runContext, cancelRun = context.WithCancel(signalContext)
	defer cancelRun()
	if err := runner.Start(runContext); err != nil {
		return fmt.Errorf("start Xray runner: %w", err)
	}
	logProfileStarted(appLogger, startupProfile, opts)

	if opts.systemProxy {
		if !systemproxy.Supported() {
			appLogger.Warn("system proxy is only supported on Windows")
		} else {
			restoreProxy, err = systemproxy.Apply(opts.listenSOCKS, opts.listenHTTP)
			if err != nil {
				return fmt.Errorf("enable system proxy: %w", err)
			}
		}
	}

	controller, err := reload.NewController(runner, prepare, xrayrunner.ValidateConfigJSON)
	if err != nil {
		return fmt.Errorf("create reload controller: %w", err)
	}
	handler := reload.NewHTTPHandler(opts.reloadToken, func(context.Context) error {
		if err := controller.Reload(runContext); err != nil {
			appLogger.Error("configuration reload failed", slog.Any("error", err))
			return err
		}
		appLogger.Info("configuration reloaded")
		return nil
	})
	endpointShutdown, err = startReloadEndpoint(opts.reloadToken, handler, func(serveErr error) {
		appLogger.Error("reload endpoint stopped unexpectedly", slog.Any("error", serveErr))
		cancelRun()
	})
	if err != nil {
		return fmt.Errorf("start reload endpoint: %w", err)
	}
	if opts.reloadToken != "" && !systemproxy.Supported() {
		appLogger.Warn("reload HTTP endpoint is only available on Windows")
	}

	reloadSignals, stopReloadSignals := subscribeReloadSignal()
	defer stopReloadSignals()
	var subscriptionTicker *time.Ticker
	var subscriptionTicks <-chan time.Time
	if opts.subURL != "" {
		subscriptionTicker = time.NewTicker(opts.subInterval)
		subscriptionTicks = subscriptionTicker.C
		defer subscriptionTicker.Stop()
	}

	for {
		select {
		case <-runContext.Done():
			appLogger.Info("shutdown signal received")
			return nil
		case received := <-reloadSignals:
			appLogger.Info("reload signal received", slog.String("signal", received.String()))
			if err := controller.Reload(runContext); err != nil {
				appLogger.Error("configuration reload failed", slog.Any("error", err))
			} else {
				appLogger.Info("configuration reloaded")
			}
		case <-subscriptionTicks:
			appLogger.Info("subscription refresh started")
			if err := controller.Reload(runContext); err != nil {
				appLogger.Error("subscription refresh failed", slog.Any("error", err))
			} else {
				appLogger.Info("subscription refreshed")
			}
		}
	}
}

func stopRunnerWithinTimeout(runner *xrayrunner.XrayRunner) error {
	result := make(chan error, 1)
	go func() {
		result <- runner.Stop()
	}()
	timer := time.NewTimer(shutdownTimeout)
	defer timer.Stop()
	select {
	case err := <-result:
		return err
	case <-timer.C:
		return fmt.Errorf("stop Xray timed out after %s: %w", shutdownTimeout, context.DeadlineExceeded)
	}
}

func logProfileStarted(appLogger *slog.Logger, profile config.Profile, opts options) {
	appLogger.Info("vclient started",
		slog.String("transport", profile.Network),
		slog.String("server", net.JoinHostPort(profile.Address, strconv.Itoa(profile.Port))),
		slog.String("socks_listen", opts.listenSOCKS),
		slog.String("http_listen", opts.listenHTTP),
	)
}

func logExperimentalTransport(appLogger *slog.Logger, profile config.Profile, opts options) {
	if profile.Network == "kcp" && opts.allowExperimental {
		appLogger.Warn("experimental transport enabled", slog.String("transport", profile.Network))
	}
}

func prepareRuntimeConfig(ctx context.Context, opts options, appLogger *slog.Logger) ([]byte, config.Profile, error) {
	var profile config.Profile
	var err error
	switch {
	case opts.subURL != "":
		profiles, fetchErr := config.FetchSubscription(ctx, opts.subURL, nil)
		if fetchErr != nil {
			appLogger.Warn("subscription unavailable; trying cached data", slog.Any("error", fetchErr))
			profiles, err = config.LoadSubscriptionCache(filepath.Join("cache", "subscription.json"))
			if err != nil {
				return nil, config.Profile{}, fmt.Errorf("fetch subscription and load cache: %w", errors.Join(fetchErr, err))
			}
		} else {
			if err := config.SaveSubscriptionCache(filepath.Join("cache", "subscription.json"), profiles); err != nil {
				return nil, config.Profile{}, fmt.Errorf("save subscription cache: %w", err)
			}
		}
		profile, err = config.SelectProfile(profiles, opts.profileName)
	case opts.link != "":
		profile, err = config.ParseVLESSLink(opts.link)
	case opts.configPath != "":
		profile, err = config.LoadProfileFromJSON(opts.configPath)
	default:
		err = errors.New("one of -config, -link, or -sub-url is required")
	}
	if err != nil {
		return nil, config.Profile{}, fmt.Errorf("load profile: %w", err)
	}

	var rules []router.Rule
	if opts.rulesPath != "" {
		rules, err = router.LoadRules(opts.rulesPath, []string{"proxy", "direct", "block"})
		if err != nil {
			return nil, config.Profile{}, fmt.Errorf("load routing rules: %w", err)
		}
	}
	var dnsSettings *dnsconfig.Config
	if opts.dnsPath != "" {
		dnsSettings, err = dnsconfig.LoadFile(opts.dnsPath, []string{"proxy", "direct", "block"})
		if err != nil {
			return nil, config.Profile{}, fmt.Errorf("load DNS config: %w", err)
		}
	}
	configJSON, err := xrayconf.BuildXrayJSON(profile, xrayconf.Options{
		SOCKSListen:       opts.listenSOCKS,
		HTTPListen:        opts.listenHTTP,
		AllowExperimental: opts.allowExperimental,
		Rules:             rules,
		DNS:               dnsSettings,
	})
	if err != nil {
		return nil, config.Profile{}, fmt.Errorf("build Xray config: %w", err)
	}
	if err := xrayrunner.ValidateConfigJSON(configJSON); err != nil {
		return nil, config.Profile{}, fmt.Errorf("validate Xray config: %w", err)
	}
	return configJSON, profile, nil
}

type shutdownFunc func() error

func (shutdown shutdownFunc) Close() error {
	return shutdown()
}

func waitForShutdown(ctx context.Context, appLogger *slog.Logger, closer io.Closer) error {
	<-ctx.Done()
	appLogger.Info("shutdown signal received")

	closed := make(chan error, 1)
	go func() {
		closed <- closer.Close()
	}()

	timer := time.NewTimer(shutdownTimeout)
	defer timer.Stop()
	select {
	case err := <-closed:
		if err != nil {
			return fmt.Errorf("shutdown application: %w", err)
		}
		return nil
	case <-timer.C:
		return fmt.Errorf("shutdown timed out after %s: %w", shutdownTimeout, context.DeadlineExceeded)
	}
}
