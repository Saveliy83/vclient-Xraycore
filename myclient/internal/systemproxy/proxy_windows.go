//go:build windows

package systemproxy

import (
	"errors"
	"fmt"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const internetSettingsPath = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

var internetSetOption = syscall.NewLazyDLL("wininet.dll").NewProc("InternetSetOptionW")

type registryValue struct {
	exists bool
	value  uint64
	text   string
}

func Apply(socksAddress, httpAddress string) (func() error, error) {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, internetSettingsPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return nil, fmt.Errorf("open Windows Internet Settings: %w", err)
	}
	enable, err := readDWORD(key, "ProxyEnable")
	if err != nil {
		_ = key.Close()
		return nil, fmt.Errorf("read original ProxyEnable: %w", err)
	}
	server, err := readString(key, "ProxyServer")
	if err != nil {
		_ = key.Close()
		return nil, fmt.Errorf("read original ProxyServer: %w", err)
	}
	proxy := "http=" + httpAddress + ";https=" + httpAddress + ";socks=" + socksAddress
	if err := key.SetDWordValue("ProxyEnable", 1); err != nil {
		_ = key.Close()
		return nil, fmt.Errorf("enable Windows system proxy: %w", err)
	}
	if err := key.SetStringValue("ProxyServer", proxy); err != nil {
		restoreErr := restoreValues(key, enable, server)
		notifyErr := notifyInternetSettingsChanged()
		closeErr := key.Close()
		return nil, fmt.Errorf("set Windows system proxy: %w", errors.Join(err, restoreErr, notifyErr, closeErr))
	}
	if err := notifyInternetSettingsChanged(); err != nil {
		restoreErr := restoreValues(key, enable, server)
		restoreNotifyErr := notifyInternetSettingsChanged()
		closeErr := key.Close()
		return nil, fmt.Errorf("notify Windows system proxy change: %w", errors.Join(err, restoreErr, restoreNotifyErr, closeErr))
	}

	restore := func() error {
		restoreErr := restoreValues(key, enable, server)
		notifyErr := notifyInternetSettingsChanged()
		closeErr := key.Close()
		if restoreErr != nil || notifyErr != nil || closeErr != nil {
			return fmt.Errorf("restore Windows system proxy: %w", errors.Join(restoreErr, notifyErr, closeErr))
		}
		return nil
	}
	return restore, nil
}

func Supported() bool {
	return true
}

func readDWORD(key registry.Key, name string) (registryValue, error) {
	value, _, err := key.GetIntegerValue(name)
	if errors.Is(err, registry.ErrNotExist) {
		return registryValue{}, nil
	}
	if err != nil {
		return registryValue{}, err
	}
	return registryValue{exists: true, value: value}, nil
}

func readString(key registry.Key, name string) (registryValue, error) {
	value, _, err := key.GetStringValue(name)
	if errors.Is(err, registry.ErrNotExist) {
		return registryValue{}, nil
	}
	if err != nil {
		return registryValue{}, err
	}
	return registryValue{exists: true, text: value}, nil
}

func restoreValues(key registry.Key, enable, server registryValue) error {
	var restoreErrors []error
	if enable.exists {
		if enable.value > uint64(^uint32(0)) {
			restoreErrors = append(restoreErrors, fmt.Errorf("restore ProxyEnable: %w", errors.New("registry value is outside DWORD range")))
		} else if err := key.SetDWordValue("ProxyEnable", uint32(enable.value)); err != nil {
			restoreErrors = append(restoreErrors, fmt.Errorf("restore ProxyEnable: %w", err))
		}
	} else if err := key.DeleteValue("ProxyEnable"); err != nil && !errors.Is(err, registry.ErrNotExist) {
		restoreErrors = append(restoreErrors, fmt.Errorf("delete ProxyEnable: %w", err))
	}
	if server.exists {
		if err := key.SetStringValue("ProxyServer", server.text); err != nil {
			restoreErrors = append(restoreErrors, fmt.Errorf("restore ProxyServer: %w", err))
		}
	} else if err := key.DeleteValue("ProxyServer"); err != nil && !errors.Is(err, registry.ErrNotExist) {
		restoreErrors = append(restoreErrors, fmt.Errorf("delete ProxyServer: %w", err))
	}
	return errors.Join(restoreErrors...)
}

func notifyInternetSettingsChanged() error {
	changed, _, err := internetSetOption.Call(0, 39, 0, 0)
	if changed == 0 {
		if err != syscall.Errno(0) {
			return fmt.Errorf("InternetSetOption(INTERNET_OPTION_SETTINGS_CHANGED): %w", err)
		}
		return fmt.Errorf("InternetSetOption(INTERNET_OPTION_SETTINGS_CHANGED): %w", syscall.EINVAL)
	}
	refreshed, _, err := internetSetOption.Call(0, 37, 0, 0)
	if refreshed == 0 {
		if err != syscall.Errno(0) {
			return fmt.Errorf("InternetSetOption(INTERNET_OPTION_REFRESH): %w", err)
		}
		return fmt.Errorf("InternetSetOption(INTERNET_OPTION_REFRESH): %w", syscall.EINVAL)
	}
	return nil
}
