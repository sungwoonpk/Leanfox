package analyze

import (
	"strings"
	"testing"
)

func TestFamily(t *testing.T) {
	cases := []struct{ cmd, want string }{
		{"git status", "git status"},
		{"git diff --stat HEAD~1", "git diff"},
		{"git -C /repo log", "git"}, // a flag is not a subcommand
		{"go test ./... -count=1", "go test"},
		{"npm run build", "npm run"},
		{"gh pr view 12 --json title", "gh pr"},
		{"ls -la /tmp", "ls"},
		{"cat file.txt", "cat"},
		{"/usr/bin/python3 -I script.py", "python3"},
		{"PYTHONPATH=x python3 -m pytest", "python3"},
		{"cd /repo && git log --oneline", "git log"},
		{"cd /repo; export A=1; go vet ./...", "go vet"},
		{"export PATH=$HOME/go/bin:$PATH && staticcheck ./...", "staticcheck"},
		{"S=/tmp/x\ncd $S && go build", "go build"},
		{"# a comment\n\ngrep -rn foo .", "grep"},
		{"sudo time env X=1 make test", "make test"},
		{"timeout 30 go test", "go test"},
		{"git log | head -5", "git log"},
		{"cd /a || echo nope", "echo"},
		{`git commit -m "fix a && b"`, "git commit"},
		{"python3 - <<'EOF'\nprint(1)\nEOF", "python3"},
		{"GIT", "git"},
		{"git Status", "git"}, // subcommands are case sensitive, so this is not one
		{"git ./script", "git"},
		{"git ", "git"},
		{"git \"quoted\"", "git"},
		{"", "shell"},
		{"   ", "shell"},
		{"cd /tmp", "shell"},
		{"X=1", "shell"},
		{"timeout", "timeout"},
		{"/", "shell"},
	}
	for _, c := range cases {
		if got := Family(c.cmd); got != c.want {
			t.Errorf("Family(%q) = %q, want %q", c.cmd, got, c.want)
		}
	}
}

func TestFamilyNeverContainsArguments(t *testing.T) {
	for _, cmd := range []string{
		`curl -H "Authorization: Bearer sk-secret" https://example.com`,
		`export TOKEN=abc123 && gh api /user`,
		`git push https://user:password@host/repo`,
	} {
		got := Family(cmd)
		for _, bad := range []string{"secret", "abc123", "password", "Bearer", "example.com"} {
			if strings.Contains(got, bad) {
				t.Errorf("Family(%q) = %q leaks %q", cmd, got, bad)
			}
		}
	}
}

func TestAssignmentAndSubcommandShapes(t *testing.T) {
	for w, want := range map[string]bool{"A=1": true, "_a1=x": true, "1A=x": false, "=x": false, "a-b=c": false, "plain": false, "A": false} {
		if got := isAssignment(w); got != want {
			t.Errorf("isAssignment(%q) = %v", w, got)
		}
	}
	for w, want := range map[string]bool{"diff": true, "run-script": true, "a:b": true, "-v": false, "./x": false, "9": false, "": false, "A": false, "a_b": false} {
		if got := isSubcommand(w); got != want {
			t.Errorf("isSubcommand(%q) = %v", w, got)
		}
	}
}
