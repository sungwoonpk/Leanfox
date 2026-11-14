package main

import (
	"bytes"
	"encoding/json"
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

func TestTheProgramHasNoNetworkCode(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output() // #nosec G204 -- fixed arguments
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range strings.Fields(string(out)) {
		if pkg == "net" || strings.HasPrefix(pkg, "net/") || strings.HasPrefix(pkg, "crypto/tls") {
			t.Errorf("the program links %s: it must not be able to send anything anywhere", pkg)
		}
	}
}

// hookCommand returns the command that hooks/hooks.json runs for an event.
func hookCommand(t *testing.T, event string) string {
	t.Helper()
	var h struct {
		Hooks map[string][]struct {
			Hooks []struct{ Command string }
		}
	}
	b, err := os.ReadFile(filepath.Join("..", "..", "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &h); err != nil {
		t.Fatal(err)
	}
	return h.Hooks[event][0].Hooks[0].Command
}

// shell runs a hook command the way Claude Code does: through a shell, with the hook's input on stdin.
func shell(t *testing.T, command, stdin string, env []string) (code int, stdout, stderr string) {
	t.Helper()
	cmd := exec.Command("sh", "-c", command) // #nosec G204 -- the command comes from hooks.json in this repository
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
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

func TestTheHooksOfThePluginWorkAsClaudeCodeRunsThem(t *testing.T) {
	bin := build(t)
	state := t.TempDir()
	path := os.Getenv("PATH")
	with := []string{"PATH=" + filepath.Dir(bin) + string(os.PathListSeparator) + path, "LEANFOX_STATE_DIR=" + state, "HOME=" + t.TempDir()}
	prompt := hookCommand(t, "UserPromptSubmit")
	start := hookCommand(t, "SessionStart")

	// Nothing is said about a prompt while the mode is off.
	if code, out, errs := shell(t, prompt, `{"session_id":"s1","prompt":"fix the bug"}`, with); code != 0 || out != "" || errs != "" {
		t.Errorf("mode off: exit %d stdout %q stderr %q", code, out, errs)
	}
	// /leanfox turns it on, and the model is told.
	code, out, errs := shell(t, prompt, `{"session_id":"s1","prompt":"/leanfox"}`, with)
	if code != 0 || errs != "" || !strings.Contains(out, "LEANFOX MODE IS ON") || !strings.Contains(out, `"hookEventName":"UserPromptSubmit"`) {
		t.Fatalf("/leanfox: exit %d stdout %q stderr %q", code, out, errs)
	}
	// A new session starts with the rules.
	if _, out, _ = shell(t, start, `{"session_id":"s2","source":"startup"}`, with); !strings.Contains(out, "LEANFOX MODE IS ON") || !strings.Contains(out, `"hookEventName":"SessionStart"`) {
		t.Errorf("session start: %q", out)
	}
	// Typed again, it turns off.
	if _, out, _ = shell(t, prompt, `{"session_id":"s1","prompt":"/leanfox"}`, with); !strings.Contains(out, "turned OFF") {
		t.Errorf("second /leanfox: %q", out)
	}
	if code, out, _ = shell(t, start, `{"session_id":"s3","source":"startup"}`, with); code != 0 || out != "" {
		t.Errorf("session start with the mode off: exit %d %q", code, out)
	}

	// Without the program on the PATH, a hook does nothing and does not fail: a prompt must never be blocked.
	without := []string{"PATH=/usr/bin:/bin", "LEANFOX_STATE_DIR=" + state, "HOME=" + t.TempDir()}
	for _, command := range []string{prompt, start} {
		if code, out, errs := shell(t, command, `{"session_id":"s1","prompt":"/leanfox"}`, without); code != 0 || out != "" || errs != "" {
			t.Errorf("no program: exit %d stdout %q stderr %q", code, out, errs)
		}
	}
}
