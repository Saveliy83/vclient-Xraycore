package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"

	xraycore "github.com/xtls/xray-core/core"

	_ "github.com/xtls/xray-core/main/distro/all"
	_ "github.com/xtls/xray-core/proxy/tun"
)

var (
	ErrEmptyConfig  = errors.New("xray config must not be empty")
	ErrRunnerActive = errors.New("xray runner is already started")
)

type Runner interface {
	Start(ctx context.Context) error
	Stop() error
}

type XrayRunner struct {
	mu         sync.Mutex
	configJSON []byte
	instance   *xraycore.Instance
}

func NewRunner(configJSON []byte) (*XrayRunner, error) {
	if len(bytes.TrimSpace(configJSON)) == 0 {
		return nil, fmt.Errorf("create Xray runner: %w", ErrEmptyConfig)
	}
	return &XrayRunner{configJSON: bytes.Clone(configJSON)}, nil
}

// Restart останавливает запущенный экземпляр, устанавливает конфиг и запускает новый.
func (runner *XrayRunner) Restart(ctx context.Context, configJSON []byte) error {
	if ctx == nil {
		return fmt.Errorf("restart Xray: %w", errors.New("context must not be nil"))
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("restart Xray: %w", err)
	}
	if err := ValidateConfigJSON(configJSON); err != nil {
		return fmt.Errorf("restart Xray: %w", err)
	}

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.instance == nil {
		previousConfig := bytes.Clone(runner.configJSON)
		runner.configJSON = bytes.Clone(configJSON)
		instance, err := newStartedInstance(ctx, runner.configJSON)
		if err != nil {
			runner.configJSON = previousConfig
			return fmt.Errorf("restart Xray: %w", err)
		}
		runner.instance = instance
		return nil
	}
	previousConfig := bytes.Clone(runner.configJSON)
	previousInstance := runner.instance
	runner.instance = nil
	if err := previousInstance.Close(); err != nil {
		runner.instance = previousInstance
		return fmt.Errorf("restart Xray: close current instance: %w", err)
	}

	runner.configJSON = bytes.Clone(configJSON)
	newInstance, err := newStartedInstance(ctx, runner.configJSON)
	if err == nil {
		runner.instance = newInstance
		return nil
	}

	runner.configJSON = previousConfig
	restoredInstance, restoreErr := newStartedInstance(context.WithoutCancel(ctx), previousConfig)
	if restoreErr == nil {
		runner.instance = restoredInstance
		return fmt.Errorf("restart Xray with new config: %w", err)
	}
	return fmt.Errorf("restart Xray with new config: %w", errors.Join(
		err,
		fmt.Errorf("restore previous Xray instance: %w", restoreErr),
	))
}

// ValidateConfigJSON полностью разбирает конфиг Xray, не останавливая текущий инстанс.
func ValidateConfigJSON(configJSON []byte) error {
	if len(bytes.TrimSpace(configJSON)) == 0 {
		return fmt.Errorf("validate Xray config: %w", ErrEmptyConfig)
	}
	if _, err := xraycore.LoadConfig("json", bytes.NewReader(configJSON)); err != nil {
		return fmt.Errorf("validate Xray config: %w", err)
	}
	return nil
}

// SetConfig меняет JSON-конфиг; для перезапуска сначала вызовите Stop.
func (runner *XrayRunner) SetConfig(configJSON []byte) error {
	if len(bytes.TrimSpace(configJSON)) == 0 {
		return fmt.Errorf("set Xray config: %w", ErrEmptyConfig)
	}

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.instance != nil {
		return fmt.Errorf("set Xray config: %w", ErrRunnerActive)
	}
	runner.configJSON = bytes.Clone(configJSON)
	return nil
}

func (runner *XrayRunner) Start(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("start Xray: %w", errors.New("context must not be nil"))
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("start Xray: %w", err)
	}

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.instance != nil {
		return fmt.Errorf("start Xray: %w", ErrRunnerActive)
	}

	instance, err := newStartedInstance(ctx, runner.configJSON)
	if err != nil {
		return fmt.Errorf("start Xray: %w", err)
	}
	runner.instance = instance
	return nil
}

func newStartedInstance(ctx context.Context, configJSON []byte) (*xraycore.Instance, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("start Xray instance: %w", err)
	}
	configuration, err := xraycore.LoadConfig("json", bytes.NewReader(configJSON))
	if err != nil {
		return nil, fmt.Errorf("load Xray JSON config: %w", err)
	}
	instance, err := xraycore.New(configuration)
	if err != nil {
		return nil, fmt.Errorf("create Xray instance: %w", err)
	}
	if err := instance.Start(); err != nil {
		if closeErr := instance.Close(); closeErr != nil {
			return nil, fmt.Errorf("start Xray instance: %w", errors.Join(err, fmt.Errorf("close failed instance: %w", closeErr)))
		}
		return nil, fmt.Errorf("start Xray instance: %w", err)
	}
	return instance, nil
}

func (runner *XrayRunner) Stop() error {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.instance == nil {
		return nil
	}
	instance := runner.instance
	runner.instance = nil
	if err := instance.Close(); err != nil {
		return fmt.Errorf("stop Xray: %w", err)
	}
	return nil
}
