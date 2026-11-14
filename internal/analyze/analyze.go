// Package analyze works out where the money of one coding-agent session went, and what could have been saved.
//
// The idea behind it is the cost of carrying. Every request sends the whole conversation again, and the provider
// charges for each token each time: once to write it into the cache, and then at the cache-read price on every later
// request. So what matters about a piece of text is not how big it is, but how big it is times how many requests came
// after it. The package measures that from the numbers the session file already records, with no guesses about prices
// and no tokenizer.
package analyze

import (
	"math"
	"path"
	"sort"
	"time"

	"github.com/sungwoonpk/leanfox/internal/pricing"
	"github.com/sungwoonpk/leanfox/internal/transcript"
)

// Options tune an analysis.
type Options struct {
	// Table prices the models. Nil means the built-in table.
	Table *pricing.Table
	// TopCarriers is how many single tool results to list. Zero means 10.
	TopCarriers int
	// HandoffTokens is the size of the note that a fresh session would start from in the split what-if. Zero means 3000.
	HandoffTokens int64
}

func (o Options) withDefaults() Options {
	if o.Table == nil {
		o.Table = pricing.NewTable(nil)
	}
	if o.TopCarriers <= 0 {
		o.TopCarriers = 10
	}
	if o.HandoffTokens <= 0 {
		o.HandoffTokens = 3000
	}
	return o
}

// A context that shrinks to under half of the one before, from at least this many tokens, is a new segment: the program
// compacted the conversation, or it was cleared or resumed.
const (
	segmentMinTokens = 20_000
	segmentDropRatio = 2
)

// Groups name what a slice of the carried context is made of.
const (
	GroupBase        = "fixed base"
	GroupToolResults = "tool results"
	GroupToolCalls   = "tool calls"
	GroupClaudeText  = "claude text"
	GroupThinking    = "thinking"
	GroupUserText    = "your text"
	GroupAttachments = "attachments"
)

// Analysis is the result for one session.
type Analysis struct {
	Session  SessionInfo `json:"session"`
	Cost     CostReport  `json:"cost"`
	Context  ContextInfo `json:"context"`
	Carry    CarryReport `json:"carry"`
	Carriers []Carrier   `json:"carriers"`
	Repeats  Repeats     `json:"repeats"`
	WhatIf   WhatIf      `json:"what_if"`
}

// SessionInfo says what was read.
type SessionInfo struct {
	File          string    `json:"file"`
	ID            string    `json:"id"`
	Version       string    `json:"version,omitempty"`
	Started       time.Time `json:"started"`
	Ended         time.Time `json:"ended"`
	Requests      int       `json:"requests"`
	SubagentCalls int       `json:"subagent_requests"`
	Segments      int       `json:"segments"`
	LinesRead     int       `json:"lines_read"`
	LinesSkipped  int       `json:"lines_skipped"`
	Truncated     bool      `json:"truncated,omitempty"`
}

// ContextInfo describes how large the conversation was at each request of the main thread.
type ContextInfo struct {
	BaseTokens   int64   `json:"base_tokens"`
	MedianTokens int64   `json:"median_tokens"`
	P90Tokens    int64   `json:"p90_tokens"`
	MaxTokens    int64   `json:"max_tokens"`
	FinalTokens  int64   `json:"final_tokens"`
	CacheHitRate float64 `json:"cache_hit_rate"`
}

// Slice is one kind of thing that was carried, and what carrying it cost.
type Slice struct {
	Group    string  `json:"group"`
	Name     string  `json:"name"`
	Items    int     `json:"items"`
	Tokens   int64   `json:"tokens"`
	CarryUSD float64 `json:"carry_usd"`
	Share    float64 `json:"share"`
}

// CarryReport splits the input-side cost of the main thread among what was carried.
type CarryReport struct {
	InputSideUSD float64 `json:"input_side_usd"`
	// Rates are the measured tokens per character of each kind of text, and per block for reasoning. A group that is
	// never carried gets zero.
	Rates []GroupRate `json:"rates"`
	// FitError is the median relative error when the context of a request is rebuilt from its parts. It says how far
	// the split can be trusted.
	FitError float64 `json:"fit_error"`
	Slices   []Slice `json:"slices"`
	// Rebuilds are requests that wrote far more to the cache than the conversation had grown, which happens when the
	// cache expired during a pause and the whole conversation had to be written again.
	Rebuilds   int     `json:"cache_rebuilds"`
	RebuildUSD float64 `json:"cache_rebuild_usd"`
	// Unexplained is what is left of the input-side cost after the slices and the rebuilds: estimation error, mostly.
	Unexplained float64 `json:"unexplained_usd"`
}

// GroupRate is the measured density of one kind of text.
type GroupRate struct {
	Group  string  `json:"group"`
	Unit   string  `json:"unit"`
	Tokens float64 `json:"tokens_per_unit"`
}

// Carrier is a single tool result that was expensive to carry.
type Carrier struct {
	Session  string  `json:"session,omitempty"`
	Tool     string  `json:"tool"`
	What     string  `json:"what"`
	Tokens   int64   `json:"tokens"`
	Carried  int     `json:"requests_carried"`
	CarryUSD float64 `json:"carry_usd"`
	Share    float64 `json:"share"`
}

// Repeats are texts that came back into the conversation although they were already there.
type Repeats struct {
	Rereads     RepeatTotal `json:"rereads"`
	SameOutputs RepeatTotal `json:"same_outputs"`
	// Rewrites are Write calls over a file that was already in the conversation, so that the whole file was carried again
	// where an Edit would have carried only the change.
	Rewrites RepeatTotal `json:"rewrites"`
	Top      []RepeatRow `json:"top"`
}

// RepeatTotal counts repeats of one kind.
type RepeatTotal struct {
	Count  int     `json:"count"`
	Tokens int64   `json:"tokens"`
	USD    float64 `json:"carry_usd"`
}

// RepeatRow is one repeated thing.
type RepeatRow struct {
	What   string  `json:"what"`
	Times  int     `json:"times"`
	Tokens int64   `json:"tokens"`
	USD    float64 `json:"carry_usd"`
}

// item is one piece of text that stayed in the conversation.
type item struct {
	group, name string
	tool, what  string
	chars       int
	units       float64 // what the density is measured against: characters, or one per block for reasoning
	at          int     // the first request, in its segment, that carried it
	digest      uint64
	reread      bool
	same        bool
	rewrite     bool // a Write over a file that was already in the conversation
	tokens      float64
	carried     int
	carry       float64
}

type segment struct {
	ctx   []int64 // tokens the model read, per request
	cw    []int64 // tokens written to the cache, per request
	items []item
}

// unit holds dollars per token, taken from what the session actually paid.
type unit struct{ read, write float64 }

// Analyze measures one session.
func Analyze(s *transcript.Session, opts Options) *Analysis {
	opts = opts.withDefaults()
	a := &Analysis{Session: SessionInfo{
		File: path.Base(s.Path), ID: s.ID, Version: s.Version, Started: s.First, Ended: s.Last,
		LinesRead: s.Lines, LinesSkipped: s.BadLines, Truncated: s.Truncated,
	}}

	segs, subTurns, allTurns := split(s)
	a.Session.SubagentCalls = subTurns
	a.Session.Segments = len(segs)
	for _, sg := range segs {
		a.Session.Requests += len(sg.ctx)
	}

	costReport(a, s, allTurns, opts.Table)
	u, mainInput := prices(allTurns, opts.Table)
	a.Carry.InputSideUSD = mainInput
	contextInfo(a, segs, allTurns)
	carry(a, segs, u, opts)
	repeats(a, segs)
	whatIf(a, segs, u, opts)
	return a
}

type turn struct {
	model  string
	tokens pricing.Tokens
	side   bool
}

// split walks the events once. It returns the segments of the main conversation, the number of sub-agent requests,
// and every request for pricing.
func split(s *transcript.Session) (segs []*segment, sub int, all []turn) {
	uses := map[string]transcript.Event{}
	var cur *segment
	var pending []item
	var prev int64
	readSeen := map[string]bool{}
	seenOutput := map[outKey]bool{}
	touched := map[string]bool{}

	start := func() {
		cur = &segment{}
		segs = append(segs, cur)
		readSeen = map[string]bool{}
		seenOutput = map[outKey]bool{}
		touched = map[string]bool{}
	}

	for i := range s.Events {
		e := &s.Events[i]
		if e.Kind == transcript.KindTurn {
			all = append(all, turn{model: e.Model, tokens: e.Tokens, side: e.Sidechain})
			if e.Sidechain {
				sub++
				continue
			}
			c := e.Tokens.Context()
			if cur == nil || (prev >= segmentMinTokens && c*segmentDropRatio < prev) {
				start()
			}
			for _, it := range pending {
				it.at = len(cur.ctx)
				cur.items = append(cur.items, it)
			}
			pending = pending[:0]
			cur.ctx = append(cur.ctx, c)
			cur.cw = append(cur.cw, e.Tokens.CacheWrite5m+e.Tokens.CacheWrite1h)
			prev = c
			continue
		}
		if e.Sidechain {
			continue
		}
		it, ok := makeItem(e, uses)
		if !ok {
			continue
		}
		if it.units == 0 {
			it.units = float64(it.chars)
		}
		// Repeats are marked while the events are in order, so that "already there" means before this one.
		markRepeats(&it, e, uses, readSeen, seenOutput, touched)
		pending = append(pending, it)
	}
	return segs, sub, all
}

type outKey struct {
	tool, what string
	digest     uint64
}

func makeItem(e *transcript.Event, uses map[string]transcript.Event) (item, bool) {
	switch e.Kind {
	case transcript.KindUserText:
		return item{group: GroupUserText, name: "messages", chars: e.Chars}, true
	case transcript.KindAttachment:
		return item{group: GroupAttachments, name: "reminders and files", chars: e.Chars}, true
	case transcript.KindAssistantText:
		return item{group: GroupClaudeText, name: "replies", chars: e.Chars}, true
	case transcript.KindToolUse:
		uses[e.ToolID] = *e
		w := what(e.Tool, e.Detail)
		return item{group: GroupToolCalls, name: w, tool: e.Tool, what: specific(e.Tool, e.Detail, w), chars: e.Chars}, true
	case transcript.KindToolResult:
		use, known := uses[e.ToolID]
		tool := e.Tool
		if !known || tool == "" {
			tool = "(unknown tool)"
		}
		w := what(tool, use.Detail)
		return item{group: GroupToolResults, name: w, tool: tool, what: specific(tool, use.Detail, w), chars: e.Chars, digest: e.Digest}, true
	case transcript.KindThinking:
		// The file keeps almost none of the reasoning text, so characters say nothing about its size. A block is the unit.
		return item{group: GroupThinking, name: "reasoning", chars: e.Chars, units: 1}, true
	}
	return item{}, false // a turn is handled by the caller
}

// specific names a single call, for lists of single things: a file is named by its base name, never its folder.
func specific(tool, detail, general string) string {
	switch tool {
	case "Read", "Edit", "Write", "NotebookEdit", "MultiEdit":
		if detail != "" {
			return tool + ": " + path.Base(detail)
		}
	}
	return general
}

// what says what a tool call was about, without arguments: the kind of shell command, the name of a file.
func what(tool, detail string) string {
	if tool == "Bash" {
		return "Bash: " + Family(detail)
	}
	return tool
}

func markRepeats(it *item, e *transcript.Event, uses map[string]transcript.Event, readSeen map[string]bool, seen map[outKey]bool, touched map[string]bool) {
	switch e.Kind {
	case transcript.KindToolUse:
		if e.Detail != "" && (e.Tool == "Read" || e.Tool == "Edit" || e.Tool == "Write" || e.Tool == "NotebookEdit" || e.Tool == "MultiEdit") {
			if e.Tool == "Write" && touched[e.Detail] {
				it.rewrite = true
			}
			touched[e.Detail] = true
		}
		if e.Tool == "Edit" || e.Tool == "Write" || e.Tool == "NotebookEdit" || e.Tool == "MultiEdit" {
			delete(readSeen, e.Detail)
		}
	case transcript.KindToolResult:
		use := uses[e.ToolID]
		if e.Tool == "Read" && use.Detail != "" {
			if readSeen[use.Detail] {
				it.reread = true
			}
			readSeen[use.Detail] = true
			return
		}
		if e.Chars >= minSameChars {
			k := outKey{tool: it.tool, what: it.what, digest: e.Digest}
			if seen[k] {
				it.same = true
			}
			seen[k] = true
		}
	}
}

// Identical output shorter than this is not worth reporting.
const minSameChars = 400

func percentile(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(math.Ceil(p*float64(len(sorted)))) - 1
	if i < 0 {
		i = 0
	}
	return sorted[i]
}

func contextInfo(a *Analysis, segs []*segment, all []turn) {
	var ctxs []int64
	var read, total int64
	for _, sg := range segs {
		ctxs = append(ctxs, sg.ctx...)
	}
	for _, t := range all {
		if t.side {
			continue
		}
		read += t.tokens.CacheRead
		total += t.tokens.Context()
	}
	if len(ctxs) == 0 {
		return
	}
	final := ctxs[len(ctxs)-1]
	sorted := append([]int64(nil), ctxs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	c := &a.Context
	c.BaseTokens = segs[0].ctx[0]
	c.MedianTokens = percentile(sorted, 0.5)
	c.P90Tokens = percentile(sorted, 0.9)
	c.MaxTokens = sorted[len(sorted)-1]
	c.FinalTokens = final
	if total > 0 {
		c.CacheHitRate = float64(read) / float64(total)
	}
}

// prices derives what one token cost in this session, from what the session paid on the main thread. It also returns
// the whole input-side cost of the main thread, which the carry split is measured against.
func prices(all []turn, tab *pricing.Table) (unit, float64) {
	var readUSD, writeUSD, inputUSD float64
	var readTok, writeTok, inputTok int64
	for _, t := range all {
		if t.side {
			continue
		}
		e, ok := tab.Lookup(t.model)
		if !ok {
			continue
		}
		c := e.Cost(t.tokens)
		readUSD += c.CacheRead
		writeUSD += c.CacheWrite
		inputUSD += c.Input
		readTok += t.tokens.CacheRead
		writeTok += t.tokens.CacheWrite5m + t.tokens.CacheWrite1h
		inputTok += t.tokens.Input
	}
	per := func(usd float64, tok int64, fallback float64) float64 {
		if tok > 0 {
			return usd / float64(tok)
		}
		return fallback
	}
	in := per(inputUSD, inputTok, 0)
	return unit{read: per(readUSD, readTok, in), write: per(writeUSD, writeTok, in)}, readUSD + writeUSD + inputUSD
}
