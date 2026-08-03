//go:build !darwin

package main

func nativeProcessEntries() ([]nativeProcessEntry, bool, error) {
	return nil, false, nil
}
