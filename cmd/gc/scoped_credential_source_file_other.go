//go:build !(darwin || linux || freebsd || openbsd || netbsd || dragonfly)

package main

import (
	"fmt"
	"os"
)

func openScopedCredentialSourceFile(path string) (*os.File, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open source env file: %w", err)
	}
	return file, nil
}
