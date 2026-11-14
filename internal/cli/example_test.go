package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungwoonpk/leanfox/internal/pricing"
)

// The example session in examples/ is made up. This file makes it, so that it is the same every time and holds nothing
// from a real session. Run `go test ./internal/cli -run TestExample -update` after changing the generator, and then
// copy the new sample from `make demo` into the README. The tests below fail until both are done.
var update = flag.Bool("update", false, "rewrite examples/demo-session.jsonl")

const examplePath = "../../examples/demo-session.jsonl"

func exampleSession(t *testing.T) string {
	t.Helper()
	rng := rand.New(rand.NewPCG(2026, 11))
	var sb strings.Builder
	put := func(v any) {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		sb.Write(b)
		sb.WriteByte('\n')
	}
	filler := func(n int, seed string) string {
		line := seed + " ok 0123456789 abcdefghij\n"
		return strings.Repeat(line, n/len(line)+1)[:n]
	}

	const (
		id       = "d3m0d3m0-0000-4000-8000-000000000001"
		model    = "claude-sonnet-5-5"
		requests = 110
	)
	at := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	ts := func() string { return at.Format("2006-01-02T15:04:05.000Z") }

	put(m{"type": "user", "sessionId": id, "version": "demo", "timestamp": ts(), "message": m{"role": "user", "content": "Add retries to the upload client, with tests."}})

	var total pricing.Tokens
	ctx, pending := int64(36_000), int64(0)
	files := []string{"/app/src/client.go", "/app/src/upload.go", "/app/src/retry.go", "/app/src/client_test.go", "/app/README.md"}
	readOnce := map[string]bool{}
	for i := range requests {
		at = at.Add(20 * time.Second)
		rebuild := false
		if i == 40 || i == 85 {
			at = at.Add(2 * time.Hour) // a pause longer than the cache lasts
			rebuild = true
		}
		var u pricing.Tokens
		u.Input, u.Output = 2, 0
		switch {
		case i == 0:
			u.CacheWrite1h = ctx
		case rebuild:
			ctx += pending
			u.CacheWrite1h = ctx
		default:
			u.CacheRead, u.CacheWrite1h = ctx, pending
			ctx += pending
		}

		// What the model writes in this request.
		text := 0
		if rng.IntN(3) == 0 {
			text = 120 + rng.IntN(500)
		}
		var callChars, resChars int
		var name string
		var input m
		switch r := rng.IntN(100); {
		case r < 30:
			cmd := []string{"go test ./...", "go vet ./...", "go build ./..."}[rng.IntN(3)]
			name, input, callChars, resChars = "Bash", m{"command": cmd}, len(cmd)+30, 1_500+rng.IntN(7_500)
		case r < 50:
			f := files[rng.IntN(len(files))]
			name, input, resChars = "Read", m{"file_path": f}, 2_000+rng.IntN(10_000)
			callChars = len(f) + 30
			if readOnce[f] && rng.IntN(2) == 0 {
				resChars = 4_321 // reading the same file again gives the same text
			}
			readOnce[f] = true
		case r < 75:
			f := files[rng.IntN(len(files))]
			body := 2_000 + rng.IntN(7_000)
			name, input, callChars, resChars = "Write", m{"file_path": f, "content": filler(body, f)}, body+60, 60
			delete(readOnce, f)
		case r < 90:
			body := 1_500 + rng.IntN(4_500)
			name, input, callChars, resChars = "Bash", m{"command": "cat > /app/src/gen.go <<'EOF'\n" + filler(body, "gen") + "EOF"}, body+40, 20
		default:
			cmd := []string{"git status", "git diff --stat", "ls -la /app/src"}[rng.IntN(3)]
			name, input, callChars, resChars = "Bash", m{"command": cmd}, len(cmd)+30, 300+rng.IntN(2_700)
			if cmd == "git status" {
				resChars = 700
			}
		}
		out := int64(float64(callChars)*0.5 + float64(text)*0.4 + 950)
		u.Output = out
		total = total.Add(u)
		pending = out + int64(float64(resChars)*0.45)

		req := fmt.Sprintf("req_%03d", i)
		usage := m{
			"input_tokens": u.Input, "output_tokens": u.Output, "cache_read_input_tokens": u.CacheRead,
			"cache_creation_input_tokens": u.CacheWrite1h, "cache_creation": m{"ephemeral_5m_input_tokens": 0, "ephemeral_1h_input_tokens": u.CacheWrite1h},
		}
		rec := func(block m) {
			put(m{"type": "assistant", "requestId": req, "sessionId": id, "timestamp": ts(), "message": m{"model": model, "usage": usage, "content": []any{block}}})
		}
		rec(m{"type": "thinking", "thinking": ""})
		if text > 0 {
			rec(m{"type": "text", "text": filler(text, "note")})
		}
		call := fmt.Sprintf("toolu_%03d", i)
		rec(m{"type": "tool_use", "id": call, "name": name, "input": input})
		resText := filler(resChars, name)
		if name == "Bash" && resChars == 700 {
			resText = filler(700, "status") // the same output every time
		}
		at = at.Add(3 * time.Second)
		put(m{"type": "user", "sessionId": id, "timestamp": ts(), "message": m{"role": "user", "content": []any{m{"type": "tool_result", "tool_use_id": call, "content": resText}}}})
	}

	e, _ := pricing.NewTable(nil).Lookup(model)
	usd := e.Cost(total).Total()
	put(m{"type": "cost-state", "sessionId": id, "totalCostUSD": usd, "modelUsage": m{model: m{
		"inputTokens": total.Input, "outputTokens": total.Output, "cacheReadInputTokens": total.CacheRead,
		"cacheCreationInputTokens": total.CacheWrite1h, "cacheCreationEphemeral5mInputTokens": 0,
		"cacheCreationEphemeral1hInputTokens": total.CacheWrite1h, "costUSD": usd,
	}}})
	return sb.String()
}

func TestExampleSessionIsTheOneTheGeneratorMakes(t *testing.T) {
	want := exampleSession(t)
	if *update {
		if err := os.MkdirAll(filepath.Dir(examplePath), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(examplePath, []byte(want), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(examplePath)
	if err != nil {
		t.Fatalf("%v (run with -update to make it)", err)
	}
	if string(got) != want {
		t.Error("examples/demo-session.jsonl is not what the generator makes; run `go test ./internal/cli -run TestExample -update`")
	}
	if len(got) > 2<<20 {
		t.Errorf("the example is %d bytes; keep it small", len(got))
	}
}

// demoReport is what `leanfox report examples/demo-session.jsonl` writes, with times in UTC so that it does not depend on
// the machine.
func demoReport(t *testing.T) string {
	t.Helper()
	old := time.Local
	time.Local = time.UTC
	defer func() { time.Local = old }()
	var out, errb bytes.Buffer
	if code := Run([]string{"report", examplePath}, &out, &errb, Env{Getenv: func(string) string { return "" }, Now: time.Now}); code != ExitOK {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	return out.String()
}

func TestReadmeSampleIsWhatTheProgramWrites(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	const open, closing = "<!-- sample:start -->\n```text\n", "```\n<!-- sample:end -->"
	text := string(readme)
	i, j := strings.Index(text, open), strings.Index(text, closing)
	if i < 0 || j < i {
		t.Fatal("the README has no sample between the sample markers")
	}
	sample := text[i+len(open) : j]
	full := demoReport(t)
	if !strings.HasPrefix(full, sample) {
		t.Errorf("the README sample is not the start of what the program writes.\nREADME:\n%s\nprogram:\n%s", sample, full)
	}
	if lines := strings.Count(sample, "\n"); lines < 15 {
		t.Errorf("the sample is only %d lines", lines)
	}
}
