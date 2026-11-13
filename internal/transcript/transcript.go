// Package transcript reads the session files that Claude Code writes under ~/.claude/projects.
//
// The format is not documented and it changes between versions, so the reader asks for as little as it can and ignores
// everything else. It needs, per line: a type, the request id, token usage, and the content blocks of messages. Every
// other field, and every record type it does not know, is skipped without an error. A line that is not JSON is counted
// and skipped. A line longer than MaxLineBytes is counted and skipped without being held in memory.
package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"time"
	"unicode/utf8"

	"github.com/sungwoonpk/leanfox/internal/pricing"
)

// Limits that keep a hostile or damaged file from using unbounded memory.
const (
	MaxLineBytes = 64 << 20
	MaxEvents    = 5_000_000
	maxDetail    = 300
)

// The limits as variables, so that a test can lower them instead of building a 64 MiB line.
var (
	maxLineBytes = MaxLineBytes
	maxEvents    = MaxEvents
)

// Kind says what an event is.
type Kind uint8

// The kinds of event, in the order a session produces them.
const (
	// KindTurn is one request to the model, with what it used. It comes once per request id.
	KindTurn Kind = iota + 1
	// KindUserText is text a person typed.
	KindUserText
	// KindAttachment is context the program added by itself: reminders, file contents, hook output.
	KindAttachment
	// KindAssistantText is text the model wrote for the person.
	KindAssistantText
	// KindThinking is reasoning the model wrote.
	KindThinking
	// KindToolUse is a tool call the model wrote.
	KindToolUse
	// KindToolResult is what a tool call returned.
	KindToolResult
)

func (k Kind) String() string {
	switch k {
	case KindTurn:
		return "turn"
	case KindUserText:
		return "user text"
	case KindAttachment:
		return "attachment"
	case KindAssistantText:
		return "assistant text"
	case KindThinking:
		return "thinking"
	case KindToolUse:
		return "tool call"
	case KindToolResult:
		return "tool result"
	}
	return "unknown"
}

// Event is one thing that happened in a session, in file order.
type Event struct {
	Kind      Kind
	Sidechain bool // written by a sub-agent, not the main conversation
	Time      time.Time

	// KindTurn.
	RequestID string
	Model     string
	Tokens    pricing.Tokens

	// Every content kind: the size in characters.
	Chars int

	// KindToolUse and KindToolResult.
	ToolID  string
	Tool    string // a result carries the name of the call it answers, when that call was seen
	Detail  string // a call: the command, file path or pattern, shortened. Empty for a result.
	IsError bool
	Digest  uint64 // a result: a hash of its text, to find identical output
	Images  int    // a result: images it held, which are not counted in Chars
}

// ModelCost is what Claude Code recorded for one model in one session.
type ModelCost struct {
	Tokens pricing.Tokens
	USD    float64
}

// CostState is a running total that Claude Code writes into the file from time to time. The last one is the final one.
type CostState struct {
	TotalUSD float64
	Models   map[string]ModelCost
}

// Session is everything read from one file.
type Session struct {
	Path      string
	ID        string
	Version   string
	Lines     int // lines that held anything
	BadLines  int // lines skipped because they were not JSON or were too long
	Truncated bool
	Events    []Event
	Costs     []CostState
	First     time.Time
	Last      time.Time
}

type rawRecord struct {
	Type        string          `json:"type"`
	RequestID   string          `json:"requestId"`
	SessionID   string          `json:"sessionId"`
	Timestamp   string          `json:"timestamp"`
	Version     string          `json:"version"`
	IsSidechain bool            `json:"isSidechain"`
	Message     *rawMessage     `json:"message"`
	Attachment  json.RawMessage `json:"attachment"`

	TotalCostUSD float64                  `json:"totalCostUSD"`
	ModelUsage   map[string]rawModelUsage `json:"modelUsage"`
}

type rawMessage struct {
	ID      string          `json:"id"`
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
	Usage   *rawUsage       `json:"usage"`
}

type rawUsage struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	CacheReadTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationTokens int64 `json:"cache_creation_input_tokens"`
	CacheCreation       *struct {
		Ephemeral5m int64 `json:"ephemeral_5m_input_tokens"`
		Ephemeral1h int64 `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
	ServerToolUse *struct {
		WebSearchRequests int64 `json:"web_search_requests"`
	} `json:"server_tool_use"`
}

type rawModelUsage struct {
	InputTokens         int64   `json:"inputTokens"`
	OutputTokens        int64   `json:"outputTokens"`
	CacheReadTokens     int64   `json:"cacheReadInputTokens"`
	CacheCreationTokens int64   `json:"cacheCreationInputTokens"`
	CacheCreation5m     int64   `json:"cacheCreationEphemeral5mInputTokens"`
	CacheCreation1h     int64   `json:"cacheCreationEphemeral1hInputTokens"`
	WebSearchRequests   int64   `json:"webSearchRequests"`
	CostUSD             float64 `json:"costUSD"`
}

type rawBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

type toolInfo struct{ name, detail string }

type parser struct {
	s      *Session
	turns  map[string]int // request id -> index of its KindTurn event
	tools  map[string]toolInfo
	blocks map[string]struct{} // tool call ids already counted, so a repeated record does not count twice
}

// ParseFile reads one session file.
func ParseFile(path string) (*Session, error) {
	f, err := os.Open(path) // #nosec G304 -- the path is the file the user asked to read
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f, path)
}

// Parse reads a session from r. path is only recorded.
func Parse(r io.Reader, path string) (*Session, error) {
	p := &parser{
		s:      &Session{Path: path},
		turns:  map[string]int{},
		tools:  map[string]toolInfo{},
		blocks: map[string]struct{}{},
	}
	br := bufio.NewReaderSize(r, 1<<20)
	for {
		line, tooLong, err := readLine(br)
		if len(line) > 0 || tooLong {
			p.line(line, tooLong)
		}
		if p.s.Truncated {
			break
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return p.s, err
		}
	}
	return p.s, nil
}

// readLine returns the next line without its newline. A line over MaxLineBytes is read to its end and dropped, and
// tooLong is true.
func readLine(br *bufio.Reader) (line []byte, tooLong bool, err error) {
	for {
		chunk, isPrefix, e := br.ReadLine()
		if e != nil {
			return line, tooLong, e
		}
		if !tooLong {
			if len(line)+len(chunk) > maxLineBytes {
				tooLong = true
				line = nil
			} else {
				line = append(line, chunk...)
			}
		}
		if !isPrefix {
			return line, tooLong, nil
		}
	}
}

func (p *parser) line(b []byte, tooLong bool) {
	s := p.s
	if tooLong {
		s.BadLines++
		return
	}
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return
	}
	var rec rawRecord
	if err := json.Unmarshal(b, &rec); err != nil {
		s.BadLines++
		return
	}
	s.Lines++
	if s.ID == "" {
		s.ID = rec.SessionID
	}
	if rec.Version != "" {
		s.Version = rec.Version
	}
	ts := parseTime(rec.Timestamp)
	if !ts.IsZero() {
		if s.First.IsZero() || ts.Before(s.First) {
			s.First = ts
		}
		if ts.After(s.Last) {
			s.Last = ts
		}
	}

	switch rec.Type {
	case "cost-state":
		p.cost(&rec)
	case "assistant", "user":
		if rec.Message != nil {
			p.message(&rec, ts)
		}
	case "attachment":
		if n := utf8.RuneCount(rec.Attachment); n > 0 {
			p.add(Event{Kind: KindAttachment, Sidechain: rec.IsSidechain, Time: ts, Chars: n})
		}
	}
}

func (p *parser) add(e Event) {
	if len(p.s.Events) >= maxEvents {
		p.s.Truncated = true
		return
	}
	p.s.Events = append(p.s.Events, e)
}

func (p *parser) cost(rec *rawRecord) {
	cs := CostState{TotalUSD: rec.TotalCostUSD, Models: make(map[string]ModelCost, len(rec.ModelUsage))}
	for model, u := range rec.ModelUsage {
		t := pricing.Tokens{
			Input: u.InputTokens, Output: u.OutputTokens, CacheRead: u.CacheReadTokens,
			CacheWrite5m: u.CacheCreation5m, CacheWrite1h: u.CacheCreation1h, WebSearches: u.WebSearchRequests,
		}
		// An older file may give only the total. Without the split, the cheaper short lifetime is the honest default.
		if t.CacheWrite5m+t.CacheWrite1h == 0 && u.CacheCreationTokens > 0 {
			t.CacheWrite5m = u.CacheCreationTokens
		}
		cs.Models[model] = ModelCost{Tokens: t, USD: u.CostUSD}
	}
	p.s.Costs = append(p.s.Costs, cs)
}

func (p *parser) message(rec *rawRecord, ts time.Time) {
	m := rec.Message
	if rec.Type == "assistant" && m.Usage != nil {
		p.turn(rec, ts)
	}
	kindText := KindUserText
	if rec.Type == "assistant" {
		kindText = KindAssistantText
	}

	content := bytes.TrimSpace(m.Content)
	if len(content) == 0 {
		return
	}
	if content[0] == '"' {
		var text string
		if json.Unmarshal(content, &text) == nil && text != "" {
			p.add(Event{Kind: kindText, Sidechain: rec.IsSidechain, Time: ts, Chars: utf8.RuneCountInString(text)})
		}
		return
	}
	var blocks []rawBlock
	if json.Unmarshal(content, &blocks) != nil {
		return
	}
	for i := range blocks {
		p.block(&blocks[i], rec, kindText, ts)
	}
}

func (p *parser) turn(rec *rawRecord, ts time.Time) {
	m := rec.Message
	u := m.Usage
	t := pricing.Tokens{
		Input: u.InputTokens, Output: u.OutputTokens, CacheRead: u.CacheReadTokens,
	}
	if u.CacheCreation != nil && u.CacheCreation.Ephemeral5m+u.CacheCreation.Ephemeral1h > 0 {
		t.CacheWrite5m, t.CacheWrite1h = u.CacheCreation.Ephemeral5m, u.CacheCreation.Ephemeral1h
	} else {
		t.CacheWrite5m = u.CacheCreationTokens
	}
	if u.ServerToolUse != nil {
		t.WebSearches = u.ServerToolUse.WebSearchRequests
	}
	if t == (pricing.Tokens{}) {
		return // a message the program made up, with no request behind it
	}
	id := rec.RequestID
	if id == "" {
		id = m.ID
	}
	if id != "" {
		if i, seen := p.turns[id]; seen {
			// Claude Code writes one record per content block and repeats the usage in each. Keep the largest
			// output count in case a streamed record was written before the final one.
			if t.Output > p.s.Events[i].Tokens.Output {
				p.s.Events[i].Tokens.Output = t.Output
			}
			return
		}
	}
	if id != "" && len(p.s.Events) < maxEvents {
		p.turns[id] = len(p.s.Events)
	}
	p.add(Event{
		Kind: KindTurn, Sidechain: rec.IsSidechain, Time: ts,
		RequestID: id, Model: m.Model, Tokens: t,
	})
}

func (p *parser) block(b *rawBlock, rec *rawRecord, kindText Kind, ts time.Time) {
	side := rec.IsSidechain
	switch b.Type {
	case "text":
		if n := utf8.RuneCountInString(b.Text); n > 0 {
			p.add(Event{Kind: kindText, Sidechain: side, Time: ts, Chars: n})
		}
	case "thinking":
		if n := utf8.RuneCountInString(b.Thinking); n > 0 {
			p.add(Event{Kind: KindThinking, Sidechain: side, Time: ts, Chars: n})
		}
	case "tool_use":
		if b.ID != "" {
			if _, dup := p.blocks[b.ID]; dup {
				return
			}
			p.blocks[b.ID] = struct{}{}
		}
		detail := toolDetail(b.Name, b.Input)
		if b.ID != "" {
			p.tools[b.ID] = toolInfo{name: b.Name, detail: detail}
		}
		p.add(Event{
			Kind: KindToolUse, Sidechain: side, Time: ts, ToolID: b.ID, Tool: b.Name, Detail: detail,
			Chars: utf8.RuneCount(b.Input),
		})
	case "tool_result":
		text, images := resultText(b.Content)
		h := fnv.New64a()
		_, _ = h.Write([]byte(text))
		info := p.tools[b.ToolUseID]
		p.add(Event{
			Kind: KindToolResult, Sidechain: side, Time: ts, ToolID: b.ToolUseID, Tool: info.name,
			Chars: utf8.RuneCountInString(text), IsError: b.IsError, Digest: h.Sum64(), Images: images,
		})
	}
}

// resultText returns the text of a tool result, which is either a string or a list of blocks, and how many images it held.
func resultText(raw json.RawMessage) (text string, images int) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", 0
	}
	if raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s) // the raw text came out of valid JSON, so a string that starts here is whole
		return s, 0
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return "", 0
	}
	var buf bytes.Buffer
	for _, b := range blocks {
		switch b.Type {
		case "text":
			buf.WriteString(b.Text)
		case "image":
			images++
		}
	}
	return buf.String(), images
}

// toolDetail picks the one field of a tool call that says what it was about, and shortens it.
func toolDetail(name string, input json.RawMessage) string {
	var key string
	switch name {
	case "Bash":
		key = "command"
	case "Read", "Edit", "Write", "NotebookEdit", "MultiEdit":
		key = "file_path"
	case "Grep", "Glob":
		key = "pattern"
	default:
		return ""
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(input, &m) != nil {
		return ""
	}
	var s string
	if json.Unmarshal(m[key], &s) != nil {
		return ""
	}
	return shorten(s, maxDetail)
}

func shorten(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// Describe is a one-line summary for error messages and tests.
func (s *Session) Describe() string {
	return fmt.Sprintf("%s: %d lines, %d events, %d skipped", s.Path, s.Lines, len(s.Events), s.BadLines)
}
