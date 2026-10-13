//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

func startReloadEndpoint(token string, handler http.Handler, failed func(error)) (func(context.Context) error, error) {
	if token == "" {
		return func(context.Context) error { return nil }, nil
	}
	listener, err := net.Listen("tcp", "127.0.0.1:9090")
	if err != nil {
		return nil, fmt.Errorf("listen for reload requests on 127.0.0.1:9090: %w", err)
	}
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       40 * time.Second,
		WriteTimeout:      40 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil && failed != nil {
				failed(fmt.Errorf("reload endpoint panic: %w", errors.New(fmt.Sprint(recovered))))
			}
		}()
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) && failed != nil {
			failed(fmt.Errorf("serve reload endpoint: %w", err))
		}
	}()
	return func(ctx context.Context) error {
		if err := server.Shutdown(ctx); err != nil {
			if closeErr := server.Close(); closeErr != nil {
				return fmt.Errorf("shutdown reload endpoint: %w", errors.Join(err, closeErr))
			}
			return fmt.Errorf("shutdown reload endpoint: %w", err)
		}
		return nil
	}, nil
}
