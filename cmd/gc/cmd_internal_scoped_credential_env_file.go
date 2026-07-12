package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gastownhall/gascity/internal/runtime/secretscrub"
	"github.com/spf13/cobra"
)

func newInternalScopedCredentialEnvFileCmd(stdout, stderr io.Writer) *cobra.Command {
	var out string
	var fromEnv []string
	cmd := &cobra.Command{
		Use:    "scoped-credential-env-file",
		Short:  "Write a scoped worker credential env file",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			entries := make(map[string]string, len(fromEnv))
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
			}
			if err := secretscrub.WriteScopedCredentialEnvFile(strings.TrimSpace(out), entries); err != nil {
				fmt.Fprintf(stderr, "gc internal scoped-credential-env-file: %v\n", err) //nolint:errcheck // best-effort stderr
				return errExit
			}
			fmt.Fprintf(stdout, "wrote scoped credential env file %s (%d key(s))\n", strings.TrimSpace(out), len(entries)) //nolint:errcheck // best-effort stdout
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "absolute output path for the scoped credential dotenv file")
	cmd.Flags().StringArrayVar(&fromEnv, "from-env", nil, "copy one credential from the process environment as KEY or KEY=SOURCE_ENV; values are never printed")
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
