//go:build !windows

package systemproxy

func Apply(socksAddress, httpAddress string) (func() error, error) {
	return func() error { return nil }, nil
}

func Supported() bool {
	return false
}
