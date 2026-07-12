package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/gastownhall/gascity/internal/doctor"
	"github.com/gastownhall/gascity/internal/processenv"
)

// supervisorProviderCredsCheck warns when the installed platform service file
// persists provider credentials directly in launchd EnvironmentVariables.
// Values are never returned: findings are key names only.
type supervisorProviderCredsCheck struct{}

func newSupervisorProviderCredsCheck() supervisorProviderCredsCheck {
	return supervisorProviderCredsCheck{}
}

func (supervisorProviderCredsCheck) Name() string { return "supervisor-provider-creds" }

func (supervisorProviderCredsCheck) CanFix() bool { return false }

func (supervisorProviderCredsCheck) WarmupEligible() bool { return false }

func (supervisorProviderCredsCheck) Fix(_ *doctor.CheckContext) error { return nil }

func (supervisorProviderCredsCheck) Run(_ *doctor.CheckContext) *doctor.CheckResult {
	path := supervisorLaunchdPlistPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &doctor.CheckResult{Name: "supervisor-provider-creds", Status: doctor.StatusOK, Message: "supervisor launchd plist not installed"}
		}
		return &doctor.CheckResult{
			Name:     "supervisor-provider-creds",
			Status:   doctor.StatusWarning,
			Severity: doctor.SeverityAdvisory,
			Message:  "could not inspect supervisor launchd env for provider credentials",
			Details:  []string{fmt.Sprintf("%s: %v", path, err)},
		}
	}
	env, err := launchdEnvironmentVariables(data)
	if err != nil {
		return &doctor.CheckResult{
			Name:     "supervisor-provider-creds",
			Status:   doctor.StatusWarning,
			Severity: doctor.SeverityAdvisory,
			Message:  "could not parse supervisor launchd env for provider credentials",
			Details:  []string{fmt.Sprintf("%s: %v", path, err)},
		}
	}
	var keys []string
	for key := range env {
		if processenv.IsProviderCredentialEnv(key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return &doctor.CheckResult{Name: "supervisor-provider-creds", Status: doctor.StatusOK, Message: "supervisor launchd env has no provider credential keys"}
	}
	return &doctor.CheckResult{
		Name:     "supervisor-provider-creds",
		Status:   doctor.StatusWarning,
		Severity: doctor.SeverityAdvisory,
		Message:  fmt.Sprintf("supervisor launchd env persists %d provider credential key(s)", len(keys)),
		Details:  keys,
		FixHint:  fmt.Sprintf("move provider credentials out of the launchd plist; once scoped worker credentials are ready, reinstall with %s=1 and keep machine-local broker inputs in a private 0600 file such as %s, then restart the supervisor", supervisorOmitProviderCredsEnv, supervisorSecretsEnvFilePath()),
	}
}

func launchdEnvironmentVariables(data []byte) (map[string]string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "dict" {
			continue
		}
		root, err := parsePlistDict(dec)
		if err != nil {
			return nil, err
		}
		env, ok := root["EnvironmentVariables"]
		if !ok || env.dict == nil {
			return nil, nil
		}
		out := make(map[string]string, len(env.dict))
		for key, val := range env.dict {
			out[key] = val.text
		}
		return out, nil
	}
}
