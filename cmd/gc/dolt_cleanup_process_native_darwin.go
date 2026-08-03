//go:build darwin

package main

import (
	"bytes"
	"encoding/binary"
	"time"

	"golang.org/x/sys/unix"
)

func nativeProcessEntries() ([]nativeProcessEntry, bool, error) {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, true, err
	}
	entries := make([]nativeProcessEntry, 0, len(procs))
	for _, proc := range procs {
		pid := int(proc.Proc.P_pid)
		if pid <= 0 {
			continue
		}
		argv, ok := darwinProcArgv(pid)
		if !ok || len(argv) == 0 {
			continue
		}
		rssBytes := int64(0)
		if proc.Eproc.Xrssize > 0 {
			rssBytes = int64(proc.Eproc.Xrssize) * int64(osPageSize())
		}
		entries = append(entries, nativeProcessEntry{
			PID:           pid,
			RSSBytes:      rssBytes,
			StartIdentity: darwinStartIdentity(proc.Proc.P_starttime),
			Argv:          argv,
		})
	}
	return entries, true, nil
}

func osPageSize() int {
	return unix.Getpagesize()
}

func darwinStartIdentity(tv unix.Timeval) string {
	if tv.Sec <= 0 {
		return ""
	}
	return time.Unix(tv.Sec, int64(tv.Usec)*1000).Format("Mon Jan _2 15:04:05 2006")
}

func darwinProcArgv(pid int) ([]string, bool) {
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil || len(raw) < 4 {
		return nil, false
	}
	argc := int(binary.LittleEndian.Uint32(raw[:4]))
	if argc <= 0 {
		return nil, false
	}
	data := raw[4:]
	if i := bytes.IndexByte(data, 0); i >= 0 {
		data = data[i+1:]
	} else {
		return nil, false
	}
	for len(data) > 0 && data[0] == 0 {
		data = data[1:]
	}
	argv := make([]string, 0, argc)
	for len(data) > 0 && len(argv) < argc {
		i := bytes.IndexByte(data, 0)
		if i < 0 {
			break
		}
		if i > 0 {
			argv = append(argv, string(data[:i]))
		}
		data = data[i+1:]
	}
	return argv, len(argv) > 0
}
