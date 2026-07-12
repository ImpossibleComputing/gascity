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

func openScopedCredentialAuditLog(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open scoped credential audit log: %w", err)
	}
	return file, nil
}
