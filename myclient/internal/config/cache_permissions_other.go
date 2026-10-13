//go:build !windows

package config

func restrictCacheFile(string) error {
	return nil
}
