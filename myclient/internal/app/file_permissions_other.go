//go:build !windows

package app

func restrictAppFile(string) error {
	return nil
}
