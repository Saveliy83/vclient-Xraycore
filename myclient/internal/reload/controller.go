package reload

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"sync"
)

// Runner перезапускает работающее ядро с подготовленной конфигурацией.
type Runner interface {
	Restart(ctx context.Context, configJSON []byte) error
}

// Controller валидирует новую конфигурацию до передачи её Runner.
type Controller struct {
	mu       sync.Mutex
	runner   Runner
	prepare  func(context.Context) ([]byte, error)
	validate func([]byte) error
}

// NewController создаёт координатор атомарно подготовленной перезагрузки.
func NewController(runner Runner, prepare func(context.Context) ([]byte, error), validate func([]byte) error) (*Controller, error) {
	if runner == nil || prepare == nil || validate == nil {
		return nil, fmt.Errorf("create reload controller: %w", errors.New("runner, prepare, and validate are required"))
	}
	return &Controller{runner: runner, prepare: prepare, validate: validate}, nil
}

// Reload сначала готовит и проверяет конфигурацию и только затем перезапускает ядро.
func (controller *Controller) Reload(ctx context.Context) error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	if ctx == nil {
		return fmt.Errorf("reload Xray: %w", errors.New("context must not be nil"))
	}
	configJSON, err := controller.prepare(ctx)
	if err != nil {
		return fmt.Errorf("prepare new Xray config: %w", err)
	}
	if err := controller.validate(configJSON); err != nil {
		return fmt.Errorf("validate new Xray config: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("reload Xray: %w", err)
	}
	if err := controller.runner.Restart(ctx, configJSON); err != nil {
		return fmt.Errorf("restart Xray: %w", err)
	}
	return nil
}

// NewHTTPHandler защищает локальную команду reload bearer-токеном в заголовке.
func NewHTTPHandler(token string, reloadFunc func(context.Context) error) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/reload", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			writer.Header().Set("Allow", http.MethodPost)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		provided := request.Header.Get("X-Reload-Token")
		if token == "" || len(provided) != len(token) ||
			subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			http.Error(writer, "unauthorized", http.StatusUnauthorized)
			return
		}
		if reloadFunc == nil {
			http.Error(writer, "reload unavailable", http.StatusServiceUnavailable)
			return
		}
		if err := reloadFunc(request.Context()); err != nil {
			http.Error(writer, "reload failed", http.StatusInternalServerError)
			return
		}
		writer.WriteHeader(http.StatusOK)
	})
	return mux
}
