package common

import (
	"os"
)

// IsFile returns true when the given path is a file and exists.
func IsFile(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}

	return !info.IsDir(), nil
}
