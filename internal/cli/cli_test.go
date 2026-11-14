package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungwoonpk/leanfox/internal/pricing"
)

type m = map[string]any

var fixedNow = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

// fixture is the text of a session file with n requests. Each request reads a command result of 6,000 characters, so the
// conversation grows. When recorded is true, the file ends with the cost that Claude Code would have written.
func fixture(t *testing.T, id, model string, n int, recorded bool) string {
	t.Helper()
	var sb strings.Builder
	put := func(v any) {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		sb.Write(b)
		sb.WriteByte('\n')
	}
	put(m{"type": "user", "sessionId": id, "timestamp": "2026-11-12T10:00:00Z", "message": m{"role": "user", "content": "run the tests"}})
	var total pricing.Tokens
	ctx := int64(30_000)
	for i := range n {
		u := m{"input_tokens": 1, "output_tokens": 50}
		tok := pricing.Tokens{Input: 1, Output: 50}
		if i == 0 {
			u["cache_creation_input_tokens"] = ctx
			u["cache_creation"] = m{"ephemeral_1h_input_tokens": ctx}
			tok.CacheWrite1h = ctx
		} else {
			u["cache_read_input_tokens"] = ctx
			u["cache_creation_input_tokens"] = 3000
			u["cache_creation"] = m{"ephemeral_1h_input_tokens": 3000}
			tok.CacheRead, tok.CacheWrite1h = ctx, 3000
			ctx += 3000
		}
		total = total.Add(tok)
		call := "toolu_" + id + string(rune('a'+i))
		put(m{"type": "assistant", "requestId": id + string(rune('a'+i)), "sessionId": id, "timestamp": "2026-11-12T10:00:01Z", "message": m{
			"model": model, "usage": u,
			"content": []any{m{"type": "tool_use", "id": call, "name": "Bash", "input": m{"command": "go test ./..."}}},
		}})
		put(m{"type": "user", "sessionId": id, "timestamp": "2026-11-12T10:00:02Z", "message": m{"role": "user", "content": []any{
			m{"type": "tool_result", "tool_use_id": call, "content": strings.Repeat("ok line\n", 750) + string(rune('a'+i))},
		}}})
	}
	if recorded {
		e, _ := pricing.NewTable(nil).Lookup(model)
		usd := e.Cost(total).Total()
		put(m{"type": "cost-state", "totalCostUSD": usd, "modelUsage": m{model: m{
			"inputTokens": total.Input, "outputTokens": total.Output, "cacheReadInputTokens": total.CacheRead,
			"cacheCreationInputTokens": total.CacheWrite1h, "cacheCreationEphemeral5mInputTokens": 0,
			"cacheCreationEphemeral1hInputTokens": total.CacheWrite1h, "costUSD": usd,
		}}})
	}
	return sb.String()
}

// home makes a Claude Code projects folder with the given files and returns an environment that points at it.
func home(t *testing.T, files map[string]string) Env {
	t.Helper()
	cfg := t.TempDir()
	for name, content := range files {
		path := filepath.Join(cfg, "projects", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, fixedNow.Add(-time.Hour), fixedNow.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	return Env{
		Getenv: func(k string) string {
			if k == "CLAUDE_CONFIG_DIR" {
				return cfg
			}
			return ""
		},
		Home: t.TempDir(),
		Now:  func() time.Time { return fixedNow },
	}
}

func run(env Env, args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	code = Run(args, &out, &errb, env)
	return code, out.String(), errb.String()
}

const sonnet = "claude-sonnet-5-5"

func TestReportOfOneSession(t *testing.T) {
	env := home(t, map[string]string{"-proj/aaaa1111.jsonl": fixture(t, "aaaa1111", sonnet, 30, true)})
	code, out, errs := run(env, "report")
	if code != ExitOK || errs != "" {
		t.Fatalf("exit %d, stderr %q", code, errs)
	}
	for _, want := range []string{
		"Session aaaa1111", "30 requests", "COST", "(recorded by Claude Code)", "check:", "exactly",
		"WHERE THE INPUT COST CAME FROM", "Bash: go test", "WHAT COULD HAVE BEEN SAVED", "HOW FAR TO TRUST THIS",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the report lacks %q:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if n := len([]rune(line)); n > 120 {
			t.Errorf("a line of %d characters: %q", n, line)
		}
	}
	if strings.Contains(out, "go test ./...") {
		t.Errorf("the report contains a command with its arguments:\n%s", out)
	}
}

func TestJSONOfOneSession(t *testing.T) {
	env := home(t, map[string]string{"-proj/aaaa1111.jsonl": fixture(t, "aaaa1111", sonnet, 30, true)})
	code, out, _ := run(env, "report", "-json")
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	var doc struct {
		Session struct{ Requests int }
		Cost    struct {
			Source   string
			TotalUSD float64 `json:"total_usd"`
		}
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if doc.Session.Requests != 30 || doc.Cost.Source != "recorded" || doc.Cost.TotalUSD <= 0 {
		t.Errorf("doc = %+v", doc)
	}
}

func TestReportOfSeveralSessions(t *testing.T) {
	env := home(t, map[string]string{
		"-proj/aaaa1111.jsonl": fixture(t, "aaaa1111", sonnet, 30, true),
		"-proj/bbbb2222.jsonl": fixture(t, "bbbb2222", sonnet, 20, false),
	})
	code, out, _ := run(env, "report")
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"2 sessions, 50 requests", "aaaa1111", "bbbb2222", "ALL OF THEM TOGETHER", "1 of 2 sessions recorded"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "Session aaaa1111") != 0 {
		t.Error("without -per-session the sessions are only in the table")
	}
	_, out, _ = run(env, "report", "-per-session")
	if strings.Count(out, "Session aaaa1111") != 1 || strings.Count(out, "Session bbbb2222") != 1 {
		t.Errorf("-per-session must write each session in full:\n%s", out)
	}
	_, out, _ = run(env, "report", "-json")
	var doc struct {
		Sessions []json.RawMessage
		Summary  struct{ Sessions, Requests int }
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil || len(doc.Sessions) != 2 || doc.Summary.Sessions != 2 || doc.Summary.Requests != 50 {
		t.Errorf("err %v doc %+v", err, doc)
	}
}

func TestFiltersAndNamedFiles(t *testing.T) {
	env := home(t, map[string]string{
		"-proj-one/aaaa1111.jsonl": fixture(t, "aaaa1111", sonnet, 30, true),
		"-proj-two/bbbb2222.jsonl": fixture(t, "bbbb2222", sonnet, 20, true),
	})
	_, out, _ := run(env, "report", "-project", "two")
	if !strings.Contains(out, "Session bbbb2222") || strings.Contains(out, "aaaa1111") {
		t.Errorf("-project:\n%s", out)
	}
	_, out, _ = run(env, "report", "-session", "aaaa")
	if !strings.Contains(out, "Session aaaa1111") || strings.Contains(out, "bbbb2222") {
		t.Errorf("-session:\n%s", out)
	}
	path := filepath.Join(env.Getenv("CLAUDE_CONFIG_DIR"), "projects", "-proj-two", "bbbb2222.jsonl")
	_, out, _ = run(env, "report", path)
	if !strings.Contains(out, "Session bbbb2222") {
		t.Errorf("a named file:\n%s", out)
	}
	code, _, errs := run(env, "report", "-since", "30m")
	if code != ExitError || !strings.Contains(errs, "no session files found") {
		t.Errorf("-since 30m should leave nothing: exit %d %q", code, errs)
	}
}

func TestNoSessionsAtAll(t *testing.T) {
	// A folder that exists and holds nothing.
	empty := home(t, nil)
	if err := os.MkdirAll(filepath.Join(empty.Getenv("CLAUDE_CONFIG_DIR"), "projects"), 0o750); err != nil {
		t.Fatal(err)
	}
	code, _, errs := run(empty, "report")
	if code != ExitError || !strings.Contains(errs, "no session files found") {
		t.Errorf("exit %d stderr %q", code, errs)
	}
	// A machine that has never run Claude Code.
	env := Env{Getenv: func(string) string { return "" }, Home: t.TempDir(), Now: func() time.Time { return fixedNow }}
	code, _, errs = run(env, "report")
	if code != ExitError || !strings.Contains(errs, "no Claude Code history at") || !strings.Contains(errs, "CLAUDE_CONFIG_DIR") {
		t.Errorf("exit %d stderr %q", code, errs)
	}
}

func TestFilesWithoutRequestsAreLeftOutAndSaid(t *testing.T) {
	env := home(t, map[string]string{
		"-proj/aaaa1111.jsonl": fixture(t, "aaaa1111", sonnet, 30, true),
		"-proj/empty.jsonl":    `{"type":"user","message":{"content":"hi"}}` + "\n",
	})
	code, out, errs := run(env, "report")
	if code != ExitOK || !strings.Contains(errs, "1 file had no requests and was left out") {
		t.Errorf("exit %d stderr %q", code, errs)
	}
	if !strings.Contains(out, "Session aaaa1111") {
		t.Errorf("stdout:\n%s", out)
	}

	env = home(t, map[string]string{
		"-proj/e1.jsonl": "{}\n", "-proj/e2.jsonl": "{}\n",
	})
	code, _, errs = run(env, "report")
	if code != ExitError || !strings.Contains(errs, "2 files had no requests and were left out") || !strings.Contains(errs, "nothing to report") {
		t.Errorf("exit %d stderr %q", code, errs)
	}
}

func TestUsageErrors(t *testing.T) {
	env := home(t, map[string]string{"-proj/aaaa1111.jsonl": fixture(t, "aaaa1111", sonnet, 30, true)})
	for name, args := range map[string][]string{
		"no command":       {},
		"unknown command":  {"frobnicate"},
		"unknown flag":     {"report", "-nope"},
		"bad since":        {"report", "-since", "soon"},
		"zero top":         {"report", "-top", "0"},
		"negative handoff": {"report", "-handoff", "-5"},
	} {
		if code, _, errs := run(env, args...); code != ExitUsage || errs == "" {
			t.Errorf("%s: exit %d stderr %q", name, code, errs)
		}
	}
	if code, _, errs := run(env, "report", "-h"); code != ExitOK || !strings.Contains(errs, "Usage: leanfox report") {
		t.Errorf("-h: exit %d stderr %q", code, errs)
	}
}

func TestVersionAndHelp(t *testing.T) {
	code, out, _ := run(Env{}, "version")
	if code != ExitOK || out != "leanfox "+Version+"\n" {
		t.Errorf("version: %d %q", code, out)
	}
	for _, a := range []string{"help", "-h", "--help"} {
		if code, out, _ := run(Env{}, a); code != ExitOK || !strings.Contains(out, "leanfox report") {
			t.Errorf("%s: %d %q", a, code, out)
		}
	}
	if _, out, _ := run(Env{}, "--version"); out != "leanfox "+Version+"\n" {
		t.Errorf("--version: %q", out)
	}
}

func TestSinceDuration(t *testing.T) {
	cases := []struct {
		in          string
		defaultRoot bool
		want        time.Duration
		ok          bool
	}{
		{"", true, 30 * 24 * time.Hour, true},
		{"", false, 0, true},
		{"0", true, 0, true},
		{"all", true, 0, true},
		{"7d", false, 7 * 24 * time.Hour, true},
		{"2w", false, 14 * 24 * time.Hour, true},
		{" 36H ", false, 36 * time.Hour, true},
		{"90m", false, 90 * time.Minute, true},
		{"-1d", false, 0, false},
		{"xd", false, 0, false},
		{"99999999d", false, 0, false},
		{"-3h", false, 0, false},
		{"soon", false, 0, false},
	}
	for _, c := range cases {
		got, err := sinceDuration(c.in, c.defaultRoot)
		if (err == nil) != c.ok || (c.ok && got != c.want) {
			t.Errorf("sinceDuration(%q, %v) = %v, %v", c.in, c.defaultRoot, got, err)
		}
	}
}

func TestRatesFile(t *testing.T) {
	env := home(t, map[string]string{"-proj/aaaa1111.jsonl": fixture(t, "aaaa1111", "mystery-1", 30, false)})
	_, out, _ := run(env, "report")
	if !strings.Contains(out, "not priced, because the model is not in the price table: mystery-1") {
		t.Errorf("without prices:\n%s", out)
	}
	rates := filepath.Join(t.TempDir(), "rates.json")
	if err := os.WriteFile(rates, []byte(`{"models":{"mystery-1":{"input":1,"output":5,"cache_read":0.1,"cache_write_5m":1.25,"cache_write_1h":2}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, _ := run(env, "report", "-rates", rates)
	if code != ExitOK || strings.Contains(out, "not priced") || !strings.Contains(out, "estimated") {
		t.Errorf("with prices: exit %d\n%s", code, out)
	}

	if code, _, errs := run(env, "report", "-rates", filepath.Join(t.TempDir(), "missing.json")); code != ExitError || errs == "" {
		t.Errorf("a missing rates file: exit %d %q", code, errs)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte(`{"models":{"x":{"inptu":1}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errs := run(env, "report", "-rates", bad); code != ExitError || !strings.Contains(errs, "inptu") {
		t.Errorf("a rates file with a typo: exit %d %q", code, errs)
	}
}

func TestAMissingNamedPath(t *testing.T) {
	code, _, errs := run(home(t, nil), "report", filepath.Join(t.TempDir(), "nope.jsonl"))
	if code != ExitError || errs == "" {
		t.Errorf("exit %d %q", code, errs)
	}
}

func TestAFileThatCannotBeReadIsSkippedWithAMessage(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read anything")
	}
	env := home(t, map[string]string{"-proj/aaaa1111.jsonl": fixture(t, "aaaa1111", sonnet, 30, true)})
	locked := filepath.Join(env.Getenv("CLAUDE_CONFIG_DIR"), "projects", "-proj", "locked.jsonl")
	if err := os.WriteFile(locked, []byte("{}\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	code, out, errs := run(env, "report")
	if code != ExitOK || !strings.Contains(errs, "skipping") || !strings.Contains(errs, "locked.jsonl") || !strings.Contains(out, "Session aaaa1111") {
		t.Errorf("exit %d stderr %q", code, errs)
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }

func TestAFailureToWriteJSONIsReported(t *testing.T) {
	env := home(t, map[string]string{"-proj/aaaa1111.jsonl": fixture(t, "aaaa1111", sonnet, 30, true)})
	var errb bytes.Buffer
	if code := Run([]string{"report", "-json"}, brokenWriter{}, &errb, env); code != ExitError || errb.Len() == 0 {
		t.Errorf("exit %d stderr %q", code, errb.String())
	}
}

func TestPlural(t *testing.T) {
	if plural(1, "file") != "1 file" || plural(2, "file") != "2 files" || plural(0, "file") != "0 files" {
		t.Error("plural is wrong")
	}
}

func TestOSEnv(t *testing.T) {
	env := OSEnv()
	if env.Getenv == nil || env.Now == nil || time.Since(env.Now()) > time.Minute {
		t.Errorf("env = %+v", env)
	}
	_ = pricing.WebSearchDollars // the package is used by the fixtures above
}

// leanEnv is an environment whose state folder is a fresh temporary one.
func leanEnv(t *testing.T, stdin string) Env {
	t.Helper()
	dir := t.TempDir()
	return Env{
		Getenv: func(k string) string {
			if k == "LEANFOX_STATE_DIR" {
				return dir
			}
			return ""
		},
		Home:  t.TempDir(),
		Now:   func() time.Time { return fixedNow },
		Stdin: strings.NewReader(stdin),
	}
}

func TestModeFromTheTerminal(t *testing.T) {
	env := leanEnv(t, "")
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"mode"}, "Leanfox mode: off\n"},
		{[]string{"mode", "status"}, "Leanfox mode: off\n"},
		{[]string{"mode", "on"}, "Leanfox mode: on\n"},
		{[]string{"mode"}, "Leanfox mode: on\n"},
		{[]string{"mode", "toggle"}, "Leanfox mode: off\n"},
		{[]string{"mode", "toggle"}, "Leanfox mode: on\n"},
		{[]string{"mode", "off"}, "Leanfox mode: off\n"},
	} {
		code, out, errs := run(env, c.args...)
		if code != ExitOK || out != c.want || errs != "" {
			t.Errorf("%v: exit %d, %q, stderr %q, want %q", c.args, code, out, errs, c.want)
		}
	}
	for _, args := range [][]string{{"mode", "sideways"}, {"mode", "on", "off"}} {
		if code, _, errs := run(env, args...); code != ExitUsage || !strings.Contains(errs, "usage:") {
			t.Errorf("%v: exit %d %q", args, code, errs)
		}
	}
}

func TestModeThatCannotBeSaved(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := leanEnv(t, "")
	env.Getenv = func(k string) string {
		if k == "LEANFOX_STATE_DIR" {
			return filepath.Join(file, "inside")
		}
		return ""
	}
	if code, _, errs := run(env, "mode", "on"); code != ExitError || errs == "" {
		t.Errorf("exit %d %q", code, errs)
	}
}

func TestTheHookTurnsTheModeOnFromTheCommand(t *testing.T) {
	env := leanEnv(t, `{"session_id":"s1","prompt":"/leanfox","transcript_path":""}`)
	code, out, errs := run(env, "hook", "user-prompt")
	if code != ExitOK || errs != "" {
		t.Fatalf("exit %d stderr %q", code, errs)
	}
	var o struct {
		HookSpecificOutput struct{ HookEventName, AdditionalContext string }
	}
	if err := json.Unmarshal([]byte(out), &o); err != nil || o.HookSpecificOutput.HookEventName != "UserPromptSubmit" ||
		!strings.Contains(o.HookSpecificOutput.AdditionalContext, "LEANFOX MODE IS ON") {
		t.Errorf("err %v\n%s", err, out)
	}
	if _, status, _ := run(env, "mode"); status != "Leanfox mode: on\n" {
		t.Errorf("the hook did not turn the mode on: %q", status)
	}

	env.Stdin = strings.NewReader(`{"session_id":"s2","source":"startup"}`)
	_, out, _ = run(env, "hook", "session-start")
	if !strings.Contains(out, `"hookEventName":"SessionStart"`) || !strings.Contains(out, "LEANFOX MODE IS ON") {
		t.Errorf("session start with the mode on:\n%s", out)
	}
}

func TestTheHookNeverBlocksAPrompt(t *testing.T) {
	// Whatever is wrong, a hook called with a known event exits 0 and prints nothing.
	for _, in := range []string{"", "not json", `{"session_id": 5}`, "[]", `{"prompt": "/leanfox off now"}`} {
		env := leanEnv(t, in)
		code, out, errs := run(env, "hook", "user-prompt")
		if code != ExitOK {
			t.Errorf("input %q: exit %d, which would block the prompt", in, code)
		}
		if errs != "" {
			t.Errorf("input %q: stderr %q without LEANFOX_DEBUG", in, errs)
		}
		if strings.Contains(in, "not json") && out != "" {
			t.Errorf("input %q printed %q", in, out)
		}
	}
	// With LEANFOX_DEBUG the reason is shown, still with exit 0.
	env := leanEnv(t, "not json")
	base := env.Getenv
	env.Getenv = func(k string) string {
		if k == "LEANFOX_DEBUG" {
			return "1"
		}
		return base(k)
	}
	if code, _, errs := run(env, "hook", "user-prompt"); code != ExitOK || !strings.Contains(errs, "leanfox hook:") {
		t.Errorf("debug: exit %d %q", code, errs)
	}
	// No input at all (a nil reader) is the same as empty input.
	env = leanEnv(t, "")
	env.Stdin = nil
	if code, out, _ := run(env, "hook", "user-prompt"); code != ExitOK || out != "" {
		t.Errorf("nil input: exit %d %q", code, out)
	}
}

func TestTheHookWithoutAKnownEventIsAnErrorButNotABlock(t *testing.T) {
	for _, args := range [][]string{{"hook"}, {"hook", "pre-tool-use"}, {"hook", "user-prompt", "extra"}} {
		code, _, errs := run(leanEnv(t, "{}"), args...)
		if code != ExitError || !strings.Contains(errs, "usage:") {
			t.Errorf("%v: exit %d %q", args, code, errs)
		}
		if code == 2 {
			t.Errorf("%v: exit 2 would block a prompt", args)
		}
	}
}

func TestOSEnvHasStdin(t *testing.T) {
	if OSEnv().Stdin == nil {
		t.Error("the real environment must give the hook its input")
	}
}
