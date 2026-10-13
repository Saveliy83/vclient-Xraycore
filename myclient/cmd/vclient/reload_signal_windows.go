//go:build windows

package main

import "os"

func subscribeReloadSignal() (chan os.Signal, func()) {
	return nil, func() {}
}
