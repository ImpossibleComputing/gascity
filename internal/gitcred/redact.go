package gitcred

import (
	"net/url"
	"strings"
)

// RedactUserinfo replaces any userinfo (user or user:password) embedded in an
// http(s)/ssh URL with "***" so a credential-bearing source never reaches a
// log line, error string, or config file. URLs without userinfo are returned
// unchanged, as are strings that do not parse as a URL (scp-form remotes such
// as git@host:org/repo carry their identity in the path and are left intact).
func RedactUserinfo(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.User == nil {
		return rawURL
	}
	// Rebuild the URL without url.User so the "***" placeholder is not
	// percent-encoded (url.URL.String would render "*" as "%2A").
	u.User = nil
	rebuilt := u.String()
	if i := strings.Index(rebuilt, "://"); i >= 0 {
		return rebuilt[:i+3] + "***@" + rebuilt[i+3:]
	}
	return "***@" + rebuilt
}
