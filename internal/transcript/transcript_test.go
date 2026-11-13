package transcript

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/sungwoonpk/leanfox/internal/pricing"
)

// jsonl joins records into the text of a session file. A string is used as it is, which lets a test write a broken line.
func jsonl(t *testing.T, recs ...any) string {
	t.Helper()
	var sb strings.Builder
	for _, r := range recs {
		if s, ok := r.(string); ok {
			sb.WriteString(s)
		} else {
			b, err := json.Marshal(r)
			if err != nil {
				t.Fatal(err)
			}
			sb.Write(b)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

type m = map[string]any

func usage(in, out, cr, cc int64) m {
	return m{"input_tokens": in, "output_tokens": out, "cache_read_input_tokens": cr, "cache_creation_input_tokens": cc}
}

func assistant(req string, u m, blocks ...m) m {
	content := make([]any, len(blocks))
	for i, b := range blocks {
		content[i] = b
	}
	return m{
		"type": "assistant", "requestId": req, "timestamp": "2026-11-01T10:00:00.000Z", "sessionId": "s1", "version": "9.9.9",
		"message": m{"model": "claude-sonnet-5-5", "content": content, "usage": u},
	}
}

func userBlocks(blocks ...m) m {
	content := make([]any, len(blocks))
	for i, b := range blocks {
		content[i] = b
	}
	return m{"type": "user", "timestamp": "2026-11-01T10:00:05.000Z", "message": m{"role": "user", "content": content}}
}

func parse(t *testing.T, text string) *Session {
	t.Helper()
	s, err := Parse(strings.NewReader(text), "test.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func kinds(s *Session) []Kind {
	out := make([]Kind, len(s.Events))
	for i, e := range s.Events {
		out[i] = e.Kind
	}
	return out
}

func TestOneRequestSplitIntoRecordsIsOneTurn(t *testing.T) {
	u := usage(3, 500, 10_000, 200)
	s := parse(t, jsonl(t,
		m{"type": "user", "message": m{"role": "user", "content": "안녕 hello"}},
		assistant("req_1", u, m{"type": "thinking", "thinking": "hmm"}),
		assistant("req_1", u, m{"type": "text", "text": "ok"}),
		assistant("req_1", u, m{"type": "tool_use", "id": "toolu_1", "name": "Bash", "input": m{"command": "go test ./..."}}),
		userBlocks(m{"type": "tool_result", "tool_use_id": "toolu_1", "content": "PASS\n"}),
	))
	want := []Kind{KindUserText, KindTurn, KindThinking, KindAssistantText, KindToolUse, KindToolResult}
	got := kinds(s)
	if len(got) != len(want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("kinds = %v, want %v", got, want)
		}
	}
	if s.Events[0].Chars != 8 { // eight characters, not eleven bytes
		t.Errorf("user text = %d chars, want 8", s.Events[0].Chars)
	}
	turn := s.Events[1]
	if turn.RequestID != "req_1" || turn.Model != "claude-sonnet-5-5" || turn.Tokens.Output != 500 || turn.Tokens.CacheRead != 10_000 {
		t.Errorf("turn = %+v", turn)
	}
	use, res := s.Events[4], s.Events[5]
	if use.Tool != "Bash" || use.Detail != "go test ./..." || use.ToolID != "toolu_1" {
		t.Errorf("use = %+v", use)
	}
	if res.Tool != "Bash" || res.ToolID != "toolu_1" || res.Chars != 5 || res.IsError {
		t.Errorf("result = %+v", res)
	}
	if s.ID != "s1" || s.Version != "9.9.9" || s.Lines != 5 || s.BadLines != 0 {
		t.Errorf("session = %s id=%q version=%q", s.Describe(), s.ID, s.Version)
	}
}

func TestUsageMapping(t *testing.T) {
	cases := []struct {
		name string
		u    m
		want pricing.Tokens
	}{
		{"split by lifetime", m{
			"input_tokens": 1, "output_tokens": 2, "cache_read_input_tokens": 3, "cache_creation_input_tokens": 9,
			"cache_creation": m{"ephemeral_5m_input_tokens": 4, "ephemeral_1h_input_tokens": 5},
		}, pricing.Tokens{Input: 1, Output: 2, CacheRead: 3, CacheWrite5m: 4, CacheWrite1h: 5}},
		{"no split means the short lifetime", usage(1, 2, 3, 9), pricing.Tokens{Input: 1, Output: 2, CacheRead: 3, CacheWrite5m: 9}},
		{"an empty split falls back to the total", m{
			"input_tokens": 1, "cache_creation_input_tokens": 7,
			"cache_creation": m{"ephemeral_5m_input_tokens": 0, "ephemeral_1h_input_tokens": 0},
		}, pricing.Tokens{Input: 1, CacheWrite5m: 7}},
		{"web searches", m{"input_tokens": 1, "server_tool_use": m{"web_search_requests": 4}}, pricing.Tokens{Input: 1, WebSearches: 4}},
	}
	for _, c := range cases {
		s := parse(t, jsonl(t, assistant("r", c.u, m{"type": "text", "text": "x"})))
		if got := s.Events[0].Tokens; got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestRepeatedRecordKeepsTheLargestOutput(t *testing.T) {
	s := parse(t, jsonl(t,
		assistant("r", usage(1, 10, 100, 0), m{"type": "text", "text": "a"}),
		assistant("r", usage(1, 90, 100, 0), m{"type": "text", "text": "b"}),
		assistant("r", usage(1, 50, 100, 0), m{"type": "text", "text": "c"}),
	))
	turns := 0
	for _, e := range s.Events {
		if e.Kind == KindTurn {
			turns++
			if e.Tokens.Output != 90 {
				t.Errorf("output = %d, want 90", e.Tokens.Output)
			}
		}
	}
	if turns != 1 {
		t.Errorf("%d turns, want 1", turns)
	}
}

func TestMessageIdIsUsedWhenThereIsNoRequestId(t *testing.T) {
	a := assistant("", usage(1, 5, 0, 0), m{"type": "text", "text": "a"})
	a["message"].(m)["id"] = "msg_1"
	b := assistant("", usage(1, 5, 0, 0), m{"type": "text", "text": "b"})
	b["message"].(m)["id"] = "msg_1"
	s := parse(t, jsonl(t, a, b))
	turns := 0
	for _, e := range s.Events {
		if e.Kind == KindTurn {
			turns++
			if e.RequestID != "msg_1" {
				t.Errorf("request id = %q", e.RequestID)
			}
		}
	}
	if turns != 1 {
		t.Errorf("%d turns, want 1", turns)
	}
}

func TestRecordsWithNoIdsAreEachATurn(t *testing.T) {
	s := parse(t, jsonl(t,
		assistant("", usage(1, 5, 0, 0), m{"type": "text", "text": "a"}),
		assistant("", usage(1, 5, 0, 0), m{"type": "text", "text": "b"}),
	))
	turns := 0
	for _, e := range s.Events {
		if e.Kind == KindTurn {
			turns++
		}
	}
	if turns != 2 {
		t.Errorf("%d turns, want 2: with nothing to tell them apart, none may be dropped as a repeat", turns)
	}
}

func TestMadeUpMessageWithNoUsageIsNotATurn(t *testing.T) {
	s := parse(t, jsonl(t, assistant("r", usage(0, 0, 0, 0), m{"type": "text", "text": "No response requested."})))
	if got := kinds(s); len(got) != 1 || got[0] != KindAssistantText {
		t.Errorf("kinds = %v, want only the text", got)
	}
}

func TestRepeatedToolCallIsCountedOnce(t *testing.T) {
	call := m{"type": "tool_use", "id": "toolu_9", "name": "Read", "input": m{"file_path": "/a/b.go"}}
	s := parse(t, jsonl(t, assistant("r", usage(1, 1, 0, 0), call), assistant("r", usage(1, 1, 0, 0), call)))
	n := 0
	for _, e := range s.Events {
		if e.Kind == KindToolUse {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d tool calls, want 1", n)
	}
}

func TestToolDetail(t *testing.T) {
	long := strings.Repeat("é", 400)
	cases := []struct {
		name  string
		input m
		want  string
	}{
		{"Bash", m{"command": "git status"}, "git status"},
		{"Bash", m{"command": long}, strings.Repeat("é", maxDetail)},
		{"Read", m{"file_path": "/x/y.go"}, "/x/y.go"},
		{"Edit", m{"file_path": "/x/y.go", "old_string": "a"}, "/x/y.go"},
		{"Write", m{"file_path": "/x/z.go"}, "/x/z.go"},
		{"Grep", m{"pattern": "TODO"}, "TODO"},
		{"Glob", m{"pattern": "**/*.go"}, "**/*.go"},
		{"WebSearch", m{"query": "something private"}, ""}, // other tools say nothing about their input
		{"Bash", m{"command": 5}, ""},
		{"Bash", m{"other": "x"}, ""},
	}
	for _, c := range cases {
		s := parse(t, jsonl(t, assistant("r", usage(1, 1, 0, 0), m{"type": "tool_use", "id": "t", "name": c.name, "input": c.input})))
		if got := s.Events[1].Detail; got != c.want {
			t.Errorf("%s %v: detail %q, want %q", c.name, c.input, got, c.want)
		}
	}
	// An input that is not an object has no detail and does not break the read.
	bad := `{"type":"assistant","requestId":"r","message":{"usage":{"input_tokens":1},"content":[{"type":"tool_use","id":"t","name":"Bash","input":"x"}]}}`
	s := parse(t, bad+"\n")
	if s.Events[1].Detail != "" || s.BadLines != 0 {
		t.Errorf("detail %q, bad lines %d", s.Events[1].Detail, s.BadLines)
	}
}

func TestToolResultShapes(t *testing.T) {
	s := parse(t, jsonl(t,
		assistant("r", usage(1, 1, 0, 0), m{"type": "tool_use", "id": "t1", "name": "Bash", "input": m{"command": "x"}}),
		userBlocks(
			m{"type": "tool_result", "tool_use_id": "t1", "content": "boom", "is_error": true},
			m{"type": "tool_result", "tool_use_id": "t2", "content": []any{
				m{"type": "text", "text": "ab"}, m{"type": "image", "source": m{"data": "xxxx"}}, m{"type": "text", "text": "cd"},
			}},
			m{"type": "tool_result", "tool_use_id": "t3"},
			m{"type": "tool_result", "tool_use_id": "t4", "content": 12},
			m{"type": "tool_result", "tool_use_id": "t5", "content": "boom"},
		),
	))
	var res []Event
	for _, e := range s.Events {
		if e.Kind == KindToolResult {
			res = append(res, e)
		}
	}
	if len(res) != 5 {
		t.Fatalf("%d results", len(res))
	}
	if !res[0].IsError || res[0].Tool != "Bash" || res[0].Chars != 4 {
		t.Errorf("first = %+v", res[0])
	}
	if res[1].Chars != 4 || res[1].Images != 1 || res[1].Tool != "" {
		t.Errorf("second = %+v, want four characters, one image and no known tool", res[1])
	}
	if res[2].Chars != 0 || res[3].Chars != 0 {
		t.Errorf("a result with no content or a number as content must be empty: %+v %+v", res[2], res[3])
	}
	if res[0].Digest != res[4].Digest || res[0].Digest == res[1].Digest {
		t.Error("identical text must give the same digest and different text a different one")
	}
}

func TestCostState(t *testing.T) {
	s := parse(t, jsonl(t,
		m{"type": "cost-state", "totalCostUSD": 1.5, "modelUsage": m{
			"claude-sonnet-5-5": m{
				"inputTokens": 10, "outputTokens": 20, "cacheReadInputTokens": 30, "cacheCreationInputTokens": 9,
				"cacheCreationEphemeral5mInputTokens": 4, "cacheCreationEphemeral1hInputTokens": 5,
				"webSearchRequests": 2, "costUSD": 1.25,
			},
			"old-model": m{"cacheCreationInputTokens": 8, "costUSD": 0.25},
		}},
		m{"type": "cost-state", "totalCostUSD": 2.0, "modelUsage": m{}},
	))
	if len(s.Costs) != 2 {
		t.Fatalf("%d cost states", len(s.Costs))
	}
	c := s.Costs[0]
	want := ModelCost{Tokens: pricing.Tokens{Input: 10, Output: 20, CacheRead: 30, CacheWrite5m: 4, CacheWrite1h: 5, WebSearches: 2}, USD: 1.25}
	if c.TotalUSD != 1.5 || c.Models["claude-sonnet-5-5"] != want {
		t.Errorf("cost state = %+v", c)
	}
	if got := c.Models["old-model"]; got.Tokens.CacheWrite5m != 8 || got.USD != 0.25 {
		t.Errorf("old model = %+v, want the whole cache write counted as the short lifetime", got)
	}
	if s.Costs[1].TotalUSD != 2.0 || len(s.Costs[1].Models) != 0 {
		t.Errorf("second = %+v", s.Costs[1])
	}
}

func TestUnknownAndBrokenLines(t *testing.T) {
	s := parse(t, jsonl(t,
		m{"type": "api-request", "whatever": []any{1, 2, 3}},
		m{"type": "mode", "mode": "x"},
		"not json at all",
		`{"type":"assistant","message":{"usage":`, // cut off
		"",
		"   ",
		m{"type": "user", "message": m{"content": "ok"}, "unknownField": true},
		m{"type": "assistant"},          // no message: fine, nothing to read from it
		m{"type": "user", "message": 7}, // a message of the wrong shape cannot be decoded, so the line counts as broken
	))
	// Broken: the text that is not JSON, the cut-off line, and the message of the wrong shape.
	// Read: the two unknown types, the user line and the assistant line with no message.
	if s.BadLines != 3 || s.Lines != 4 {
		t.Errorf("bad %d lines %d, want 3 bad and 4 read", s.BadLines, s.Lines)
	}
	if got := kinds(s); len(got) != 1 || got[0] != KindUserText {
		t.Errorf("kinds = %v", got)
	}
}

func TestContentOfTheWrongShapeIsSkipped(t *testing.T) {
	s := parse(t, jsonl(t,
		m{"type": "user", "message": m{"content": 5}},
		m{"type": "user", "message": m{"content": []any{"a string, not a block"}}},
		m{"type": "user", "message": m{"content": ""}},
		m{"type": "user", "message": m{}},
		m{"type": "assistant", "message": m{"content": []any{m{"type": "text", "text": ""}, m{"type": "thinking", "thinking": ""}, m{"type": "future-block"}}}},
	))
	if len(s.Events) != 0 || s.BadLines != 0 {
		t.Errorf("events %v, bad %d", s.Events, s.BadLines)
	}
}

func TestAttachmentAndTimes(t *testing.T) {
	s := parse(t, jsonl(t,
		m{"type": "attachment", "timestamp": "2026-11-01T09:00:00Z", "attachment": m{"type": "x", "content": "hello"}},
		m{"type": "attachment", "attachment": nil},
		m{"type": "user", "timestamp": "2026-11-01T11:30:00.250Z", "message": m{"content": "hi"}},
		m{"type": "user", "timestamp": "garbage", "message": m{"content": "hi"}},
		m{"type": "user", "timestamp": "2026-11-01T10:00:00Z", "message": m{"content": "hi"}},
	))
	if s.Events[0].Kind != KindAttachment || s.Events[0].Chars != len(`{"type":"x","content":"hello"}`) {
		t.Errorf("attachment = %+v", s.Events[0])
	}
	if s.First.Hour() != 9 || s.Last.Hour() != 11 || s.Last.Nanosecond() != 250_000_000 {
		t.Errorf("first %v last %v", s.First, s.Last)
	}
	if !s.Events[3].Time.IsZero() {
		t.Error("a time that cannot be read must be zero, not an error")
	}
}

func TestSidechainIsMarked(t *testing.T) {
	a := assistant("r", usage(1, 1, 0, 0), m{"type": "text", "text": "x"})
	a["isSidechain"] = true
	s := parse(t, jsonl(t, a))
	for _, e := range s.Events {
		if !e.Sidechain {
			t.Errorf("%v event is not marked as a sub-agent's", e.Kind)
		}
	}
}

func TestLineEndingsAndMissingFinalNewline(t *testing.T) {
	text := `{"type":"user","message":{"content":"a"}}` + "\r\n" + `{"type":"user","message":{"content":"b"}}`
	s := parse(t, text)
	if len(s.Events) != 2 || s.BadLines != 0 {
		t.Errorf("events %d bad %d", len(s.Events), s.BadLines)
	}
}

func TestLongLineUnderTheCapIsRead(t *testing.T) {
	big := strings.Repeat("x", 3<<20) // longer than the reader's own buffer
	s := parse(t, jsonl(t, m{"type": "user", "message": m{"content": big}}, m{"type": "user", "message": m{"content": "after"}}))
	if len(s.Events) != 2 || s.Events[0].Chars != 3<<20 || s.BadLines != 0 {
		t.Errorf("events %d bad %d", len(s.Events), s.BadLines)
	}
}

func TestLineOverTheCapIsSkippedAndReadingGoesOn(t *testing.T) {
	old := maxLineBytes
	maxLineBytes = 2048
	defer func() { maxLineBytes = old }()

	huge := jsonl(t, m{"type": "user", "message": m{"content": strings.Repeat("y", 1<<20)}})
	s := parse(t, jsonl(t, m{"type": "user", "message": m{"content": "before"}})+huge+jsonl(t, m{"type": "user", "message": m{"content": "after"}}))
	if s.BadLines != 1 || len(s.Events) != 2 || s.Events[1].Chars != 5 {
		t.Errorf("bad %d events %+v", s.BadLines, s.Events)
	}
}

func TestEventCapStopsTheRead(t *testing.T) {
	old := maxEvents
	maxEvents = 3
	defer func() { maxEvents = old }()

	var recs []any
	for range 10 {
		recs = append(recs, m{"type": "user", "message": m{"content": "x"}})
	}
	s := parse(t, jsonl(t, recs...))
	if !s.Truncated || len(s.Events) != 3 {
		t.Errorf("truncated %v events %d", s.Truncated, len(s.Events))
	}

	// A turn that would pass the cap is not half recorded: the id must not point at an event that was never stored.
	s = parse(t, jsonl(t,
		m{"type": "user", "message": m{"content": "x"}},
		m{"type": "user", "message": m{"content": "x"}},
		m{"type": "user", "message": m{"content": "x"}},
		assistant("r", usage(1, 1, 0, 0), m{"type": "text", "text": "x"}),
		assistant("r", usage(1, 2, 0, 0), m{"type": "text", "text": "x"}),
	))
	if !s.Truncated || len(s.Events) != 3 {
		t.Errorf("truncated %v events %d", s.Truncated, len(s.Events))
	}
}

func TestReadErrorIsReturnedWithWhatWasRead(t *testing.T) {
	boom := errors.New("disk fell over")
	r := iotest.ErrReader(boom)
	s, err := Parse(r, "x")
	if !errors.Is(err, boom) || s == nil {
		t.Errorf("err %v session %v", err, s)
	}
}

func TestParseFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte(jsonl(t, m{"type": "user", "message": m{"content": "hi"}})), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := ParseFile(path)
	if err != nil || s.Path != path || len(s.Events) != 1 {
		t.Errorf("err %v session %+v", err, s)
	}
	if _, err := ParseFile(path + ".missing"); err == nil {
		t.Error("a missing file must be an error")
	}
}

func TestDescribe(t *testing.T) {
	s := parse(t, jsonl(t, m{"type": "user", "message": m{"content": "hi"}}, "junk"))
	if got, want := s.Describe(), "test.jsonl: 1 lines, 1 events, 1 skipped"; got != want {
		t.Errorf("Describe = %q, want %q", got, want)
	}
}

func TestKindNames(t *testing.T) {
	for k := KindTurn; k <= KindToolResult; k++ {
		if k.String() == "unknown" || k.String() == "" {
			t.Errorf("kind %d has no name", k)
		}
	}
	if Kind(0).String() != "unknown" {
		t.Error("an unset kind must say so")
	}
}
