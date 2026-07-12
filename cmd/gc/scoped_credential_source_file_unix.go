//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// openScopedCredentialSourceFile opens path once without following symlinks so
// readScopedCredentialSourceEnvFile validates and reads the same inode. This is
// credential-handling code: a separate stat-then-read would allow a symlink or
// rename race between the privacy check and the actual read.
func openScopedCredentialSourceFile(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("source env file %q must not be a symlink", path)
		}
		return nil, fmt.Errorf("open source env file: %w", err)
	}
	return os.NewFile(uintptr(fd), path), nil
}
