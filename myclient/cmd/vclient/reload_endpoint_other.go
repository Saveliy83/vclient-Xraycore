//go:build !windows

package main

import (
	"context"
	"net/http"
)

func startReloadEndpoint(token string, handler http.Handler, failed func(error)) (func(context.Context) error, error) {
	return func(context.Context) error { return nil }, nil
}
