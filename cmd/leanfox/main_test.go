package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests run the real program, as a separate process, on a small session file.

func build(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "leanfox")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil { // #nosec G204 -- fixed arguments
		t.Fatalf("build failed: %v\n%s", err, out)
	}
	return bin
}

const session = `{"type":"user","sessionId":"e2e00001","timestamp":"2026-11-12T10:00:00Z","message":{"role":"user","content":"hello"}}
{"type":"assistant","requestId":"r1","sessionId":"e2e00001","timestamp":"2026-11-12T10:00:01Z","message":{"model":"claude-sonnet-5-5","usage":{"input_tokens":1,"output_tokens":20,"cache_creation_input_tokens":30000,"cache_creation":{"ephemeral_1h_input_tokens":30000}},"content":[{"type":"text","text":"hi"}]}}
{"type":"assistant","requestId":"r2","sessionId":"e2e00001","timestamp":"2026-11-12T10:00:09Z","message":{"model":"claude-sonnet-5-5","usage":{"input_tokens":1,"output_tokens":20,"cache_read_input_tokens":30000,"cache_creation_input_tokens":500,"cache_creation":{"ephemeral_1h_input_tokens":500}},"content":[{"type":"text","text":"done"}]}}
`

func run(t *testing.T, bin string, env []string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	cmd := exec.Command(bin, args...) // #nosec G204 -- the binary was built by this test
	cmd.Env = append(os.Environ(), env...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), out.String(), errb.String()
	} else if err != nil {
		t.Fatal(err)
	}
	return 0, out.String(), errb.String()
}

func TestTheProgramRuns(t *testing.T) {
	bin := build(t)
	cfg := t.TempDir()
	path := filepath.Join(cfg, "projects", "-demo", "e2e00001.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(session), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{"CLAUDE_CONFIG_DIR=" + cfg}

	code, out, _ := run(t, bin, env, "version")
	if code != 0 || !strings.HasPrefix(out, "leanfox ") {
		t.Errorf("version: %d %q", code, out)
	}

	code, out, errs := run(t, bin, env, "report", "-since", "0")
	if code != 0 || errs != "" || !strings.Contains(out, "Session e2e00001") || !strings.Contains(out, "2 requests") {
		t.Errorf("report: exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errs)
	}

	code, out, _ = run(t, bin, env, "report", "-json", path)
	if code != 0 || !strings.Contains(out, `"requests": 2`) {
		t.Errorf("json: exit %d\n%s", code, out)
	}

	if code, _, errs = run(t, bin, env, "report", "-since", "0", "-project", "nothing-matches"); code != 1 || errs == "" {
		t.Errorf("no match: exit %d %q", code, errs)
	}
	if code, _, errs = run(t, bin, env, "report", "-bogus"); code != 2 || errs == "" {
		t.Errorf("bad flag: exit %d %q", code, errs)
	}
	if code, _, _ = run(t, bin, env); code != 2 {
		t.Errorf("no arguments: exit %d", code)
	}
}
