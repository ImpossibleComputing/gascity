package gitcred

import "testing"

func TestRedactUserinfo(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"https://github.com/org/repo", "https://github.com/org/repo"},
		{"https://user:ghp_secret@github.com/org/repo", "https://***@github.com/org/repo"},
		{"https://ghp_secret@github.com/org/repo", "https://***@github.com/org/repo"},
		{"git@github.com:org/repo.git", "git@github.com:org/repo.git"},
		{"file:///home/u/repo", "file:///home/u/repo"},
	}
	for _, tc := range tests {
		if got := RedactUserinfo(tc.in); got != tc.want {
			t.Errorf("RedactUserinfo(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
