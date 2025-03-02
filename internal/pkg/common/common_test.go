package common

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsFile(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)

	testCases := []struct {
		name     string
		setup    func() (string, error)
		expected bool
		hasError bool
	}{
		{
			name: "File exists",
			setup: func() (string, error) {
				file, err := os.CreateTemp("", "testfile")
				if err != nil {
					return "", err
				}
				defer file.Close()
				return file.Name(), nil
			},
			expected: true,
			hasError: false,
		},
		{
			name: "Directory exists",
			setup: func() (string, error) {
				dir, err := os.MkdirTemp("", "testdir")
				if err != nil {
					return "", err
				}
				return dir, nil
			},
			expected: false,
			hasError: false,
		},
		{
			name: "File does not exist",
			setup: func() (string, error) {
				return "/non/existent/file", nil
			},
			expected: false,
			hasError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			path, err := tc.setup()
			r.NoError(err)

			result, err := IsFile(path)
			if tc.hasError {
				r.Error(err)
			} else {
				r.NoError(err)
			}
			a.Equal(tc.expected, result)

			if !tc.hasError {
				os.Remove(path)
			}
		})
	}
}
