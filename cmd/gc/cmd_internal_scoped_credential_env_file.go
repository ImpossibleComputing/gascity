package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/gastownhall/gascity/internal/processenv"
	"github.com/gastownhall/gascity/internal/runtime/secretscrub"
	"github.com/spf13/cobra"
)

func newInternalScopedCredentialEnvFileCmd(stdout, stderr io.Writer) *cobra.Command {
	var out string
	var fromEnv []string
	var sourceEnvFile string
	var fromEnvFile []string
	var auditLog string
	cmd := &cobra.Command{
		Use:          "scoped-credential-env-file",
		Short:        "Write a scoped worker credential env file",
		Hidden:       true,
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			entries := make(map[string]string, len(fromEnv)+len(fromEnvFile))
			auditSources := make([]scopedCredentialAuditSource, 0, len(fromEnv)+len(fromEnvFile))
			for _, spec := range fromEnv {
				key, source, err := parseScopedCredentialFromEnvSpec(spec)
				if err != nil {
					fmt.Fprintf(stderr, "gc internal scoped-credential-env-file: %v\n", err) //nolint:errcheck // best-effort stderr
					return errExit
				}
				if _, exists := entries[key]; exists {
					fmt.Fprintf(stderr, "gc internal scoped-credential-env-file: duplicate output key %s\n", key) //nolint:errcheck // best-effort stderr
					return errExit
				}
				value, ok := os.LookupEnv(source)
				if !ok || strings.TrimSpace(value) == "" {
					fmt.Fprintf(stderr, "gc internal scoped-credential-env-file: source env %s for key %s is unset or empty\n", source, key) //nolint:errcheck // best-effort stderr
					return errExit
				}
				entries[key] = value
				auditSources = append(auditSources, scopedCredentialAuditSource{Key: key, Kind: "env", SourceKey: source})
			}
			if len(fromEnvFile) > 0 {
				sourceEntries, err := readScopedCredentialSourceEnvFile(sourceEnvFile)
				if err != nil {
					fmt.Fprintf(stderr, "gc internal scoped-credential-env-file: %v\n", err) //nolint:errcheck // best-effort stderr
					return errExit
				}
				for _, spec := range fromEnvFile {
					key, source, err := parseScopedCredentialFromEnvSpec(spec)
					if err != nil {
						fmt.Fprintf(stderr, "gc internal scoped-credential-env-file: %v\n", err) //nolint:errcheck // best-effort stderr
						return errExit
					}
					if _, exists := entries[key]; exists {
						fmt.Fprintf(stderr, "gc internal scoped-credential-env-file: duplicate output key %s\n", key) //nolint:errcheck // best-effort stderr
						return errExit
					}
					value, ok := sourceEntries[source]
					if !ok || strings.TrimSpace(value) == "" {
						fmt.Fprintf(stderr, "gc internal scoped-credential-env-file: source env-file key %s for key %s is unset or empty\n", source, key) //nolint:errcheck // best-effort stderr
						return errExit
					}
					entries[key] = value
					auditSources = append(auditSources, scopedCredentialAuditSource{Key: key, Kind: "env-file", SourceKey: source})
				}
			}
			if err := secretscrub.WriteScopedCredentialEnvFile(strings.TrimSpace(out), entries); err != nil {
				fmt.Fprintf(stderr, "gc internal scoped-credential-env-file: %v\n", err) //nolint:errcheck // best-effort stderr
				return errExit
			}
			if err := appendScopedCredentialAuditLog(strings.TrimSpace(auditLog), strings.TrimSpace(out), entries, auditSources); err != nil {
				fmt.Fprintf(stderr, "gc internal scoped-credential-env-file: %v\n", err) //nolint:errcheck // best-effort stderr
				return errExit
			}
			fmt.Fprintf(stdout, "wrote scoped credential env file %s (%d key(s))\n", strings.TrimSpace(out), len(entries)) //nolint:errcheck // best-effort stdout
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "absolute output path for the scoped credential dotenv file")
	cmd.Flags().StringArrayVar(&fromEnv, "from-env", nil, "copy one credential from the process environment as KEY or KEY=SOURCE_ENV; values are never printed")
	cmd.Flags().StringVar(&sourceEnvFile, "source-env-file", "", "absolute private dotenv file to read --from-env-file source keys from; values are never printed")
	cmd.Flags().StringArrayVar(&fromEnvFile, "from-env-file", nil, "copy one credential from --source-env-file as KEY or KEY=SOURCE_KEY; values are never printed")
	cmd.Flags().StringVar(&auditLog, "audit-log", "", "optional absolute private JSONL audit log path; records key names/paths only, never values")
	return cmd
}

func parseScopedCredentialFromEnvSpec(spec string) (key, source string, err error) {
	key, source, hasSource := strings.Cut(strings.TrimSpace(spec), "=")
	key = strings.TrimSpace(key)
	if !hasSource {
		source = key
	} else {
		source = strings.TrimSpace(source)
	}
	if key == "" {
		return "", "", fmt.Errorf("--from-env entry has an empty output key")
	}
	if source == "" {
		return "", "", fmt.Errorf("--from-env entry for key %s has an empty source env", key)
	}
	return key, source, nil
}

func readScopedCredentialSourceEnvFile(path string) (map[string]string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("--source-env-file is required when --from-env-file is used")
	}
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("--source-env-file must be an absolute path")
	}
	file, err := openScopedCredentialSourceFile(path)
	if err != nil {
		return nil, err
	}
	defer file.Close() //nolint:errcheck // read-only descriptor; close errors do not affect parsed data

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat source env file: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("source env file %q is a directory", path)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("source env file %q must not be group/world accessible", path)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("read source env file: %w", err)
	}
	entries, err := processenv.ParseEnvFile(string(data))
	if err != nil {
		return nil, fmt.Errorf("parse source env file: invalid dotenv syntax")
	}
	return entries, nil
}

type scopedCredentialAuditSource struct {
	Key       string `json:"key"`
	Kind      string `json:"kind"`
	SourceKey string `json:"source_key"`
}

type scopedCredentialAuditEvent struct {
	Time    string                        `json:"time"`
	Action  string                        `json:"action"`
	Out     string                        `json:"out"`
	Keys    []string                      `json:"keys"`
	Sources []scopedCredentialAuditSource `json:"sources"`
}

func appendScopedCredentialAuditLog(path, out string, entries map[string]string, sources []scopedCredentialAuditSource) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("--audit-log must be an absolute path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create scoped credential audit log dir: %w", err)
	}
	file, err := openScopedCredentialAuditLog(path)
	if err != nil {
		return err
	}
	defer file.Close() //nolint:errcheck // append-only audit log; write/fsync errors are handled below

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat scoped credential audit log: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("scoped credential audit log %q is a directory", path)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("scoped credential audit log %q must not be group/world accessible", path)
	}

	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	sortedSources := append([]scopedCredentialAuditSource(nil), sources...)
	sort.Slice(sortedSources, func(i, j int) bool {
		if sortedSources[i].Key != sortedSources[j].Key {
			return sortedSources[i].Key < sortedSources[j].Key
		}
		if sortedSources[i].Kind != sortedSources[j].Kind {
			return sortedSources[i].Kind < sortedSources[j].Kind
		}
		return sortedSources[i].SourceKey < sortedSources[j].SourceKey
	})
	event := scopedCredentialAuditEvent{
		Time:    time.Now().UTC().Format(time.RFC3339Nano),
		Action:  "write-scoped-credential-env-file",
		Out:     out,
		Keys:    keys,
		Sources: sortedSources,
	}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode scoped credential audit log event: %w", err)
	}
	data = append(data, '\n')
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("append scoped credential audit log: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync scoped credential audit log: %w", err)
	}
	return nil
}
