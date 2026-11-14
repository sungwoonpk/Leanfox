package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sungwoonpk/leanfox/internal/analyze"
	"github.com/sungwoonpk/leanfox/internal/pricing"
)

func TestFormatting(t *testing.T) {
	for in, want := range map[float64]string{0: "$0.00", 0.004: "$0.0040", 0.005: "$0.01", 12.345: "$12.35", -2.5: "-$2.50", 1234.5: "$1234.50"} {
		if got := money(in); got != want {
			t.Errorf("money(%v) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[int64]string{0: "0", 999: "999", 1000: "1.0k", 9_999: "10.0k", 10_000: "10k", 371_000: "371k", 999_999: "1000k", 1_500_000: "1.5M", -2500: "-2.5k"} {
		if got := tokens(in); got != want {
			t.Errorf("tokens(%d) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[float64]string{0: "0%", 0.004: "0.4%", 0.0149: "1%", 0.5: "50%", 0.994: "99%", -0.003: "-0.3%", 1: "100%"} {
		if got := pct(in); got != want {
			t.Errorf("pct(%v) = %q, want %q", in, got, want)
		}
	}
	if plural(1, "call") != "1 call" || plural(0, "call") != "0 calls" || plural(7, "call") != "7 calls" {
		t.Error("plural is wrong")
	}
	for in, want := range map[time.Duration]string{5 * time.Minute: "5m", 90 * time.Minute: "1h 30m", 25 * time.Hour: "1d 1h", 3*24*time.Hour + 18*time.Hour: "3d 18h"} {
		if got := duration(in); got != want {
			t.Errorf("duration(%v) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"9e34b958-314b-4232.jsonl": "9e34b958", "short.jsonl": "short", "": ""} {
		if got := shortID(in); got != want {
			t.Errorf("shortID(%q) = %q, want %q", in, got, want)
		}
	}
	if clip("abcdef", 4) != "abc…" || clip("abc", 4) != "abc" || clip("한국어입니다", 3) != "한국…" {
		t.Error("clip is wrong")
	}
	if agreement(0) != "exactly" || agreement(0.00001) != "exactly" || agreement(-0.5) != "to within 0.5000%" {
		t.Errorf("agreement = %q %q %q", agreement(0), agreement(0.00001), agreement(-0.5))
	}
	if maxAbs(1, -2) != -2 || maxAbs(-3, 2) != -3 {
		t.Error("maxAbs is wrong")
	}
	if ifTrue(true, "a", "b") != "a" || ifTrue(false, "a", "b") != "b" {
		t.Error("ifTrue is wrong")
	}
}

func fullAnalysis() *analyze.Analysis {
	return &analyze.Analysis{
		Session: analyze.SessionInfo{
			File: "abcdef12-0000.jsonl", Requests: 120, Segments: 2, LinesSkipped: 3, Truncated: true,
			Started: time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC), Ended: time.Date(2026, 11, 1, 11, 30, 0, 0, time.UTC),
		},
		Cost: analyze.CostReport{
			Source: analyze.SourceRecorded, TotalUSD: 12, Unsplit: 1, SubagentUSD: 0.5,
			Parts: pricing.Cost{CacheRead: 6, Output: 2, CacheWrite: 2.5, Input: 0.4, WebSearch: 0.1},
			Check: []analyze.Check{
				{Model: "claude-sonnet-5-5", Verified: true, DiffPercent: 0.00001},
				{Model: "claude-haiku-5-5", Verified: false, DiffPercent: -3},
			},
			Unknown: []string{"mystery-1"}, Coverage: 0.97,
		},
		Context: analyze.ContextInfo{BaseTokens: 38_000, MedianTokens: 300_000, P90Tokens: 800_000, MaxTokens: 950_000, CacheHitRate: 0.99},
		Carry: analyze.CarryReport{
			InputSideUSD: 8.4, FitError: 0.012, Rebuilds: 2, RebuildUSD: 1.2, Unexplained: 0.5,
			Slices: []analyze.Slice{
				{Group: analyze.GroupToolCalls, Name: "Write", Items: 10, Tokens: 90_000, CarryUSD: 2, Share: 2 / 8.4},
				{Group: analyze.GroupToolCalls, Name: "Bash: cat", Items: 5, Tokens: 20_000, CarryUSD: 1, Share: 1 / 8.4},
				{Group: analyze.GroupToolCalls, Name: "Edit", Items: 5, Tokens: 20_000, CarryUSD: 0.5, Share: 0.5 / 8.4},
				{Group: analyze.GroupToolCalls, Name: "Bash: ls", Items: 5, Tokens: 20_000, CarryUSD: 0.4, Share: 0.4 / 8.4},
				{Group: analyze.GroupToolCalls, Name: "Bash: tiny", Items: 1, Tokens: 10, CarryUSD: 0.001, Share: 0.001 / 8.4},
				{Group: analyze.GroupToolResults, Name: "Bash: go test", Items: 8, Tokens: 30_000, CarryUSD: 1.5, Share: 1.5 / 8.4},
				{Group: analyze.GroupBase, Name: "base", Items: 1, Tokens: 38_000, CarryUSD: 0.8, Share: 0.8 / 8.4},
				{Group: analyze.GroupClaudeText, Name: "replies", Items: 50, Tokens: 9_000, CarryUSD: 0.3, Share: 0.3 / 8.4},
			},
		},
		Carriers: []analyze.Carrier{
			{What: "Read: main.go", Tokens: 15_000, Carried: 200, CarryUSD: 0.4, Share: 0.05},
			{What: "Bash: ls", Tokens: 5_000, Carried: 90, CarryUSD: 0.1, Share: 0.01},
		},
		Repeats: analyze.Repeats{
			Rereads:     analyze.RepeatTotal{Count: 3, Tokens: 4000, USD: 0.2},
			SameOutputs: analyze.RepeatTotal{Count: 2, Tokens: 900, USD: 0.05},
			Top:         []analyze.RepeatRow{{What: "Read: main.go", Times: 3, USD: 0.2}},
		},
		WhatIf: analyze.WhatIf{
			HandoffTokens: 3000,
			Cap:           []analyze.CapOutcome{{LimitTokens: 1000, Affected: 7, SavedUSD: 0.6}},
			Split:         []analyze.SplitOutcome{{EveryRequests: 100, Restarts: 1, SavedUSD: -0.2}},
		},
	}
}

func render(f func(*bytes.Buffer)) string {
	var b bytes.Buffer
	f(&b)
	return b.String()
}

func TestSessionReportShowsEverySection(t *testing.T) {
	out := render(func(b *bytes.Buffer) { Session(b, fullAnalysis()) })
	for _, want := range []string{
		"Session abcdef12  (120 requests, 2 segments, started 2026-11-01", "spanning 2h 30m",
		"note: 3 lines of the file could not be read and it was cut short",
		"COST  $12.00  (recorded by Claude Code)",
		"cache reads", "web search", "unsplit",
		"check: the price table reproduces the cost Claude Code recorded, exactly",
		"unverified rates (claude-haiku-5-5) differ from the recorded cost by 3.00%",
		"not priced, because the model is not in the price table: mystery-1",
		"sub-agents: $0.50 of the cost above",
		"CONTEXT", "starts at 38k tokens, median 300k",
		"WHERE THE INPUT COST CAME FROM  ($8.40",
		"tool calls", "tool results", "cache rebuilds", "2 times", "fixed base",
		"Write", "5 calls", "Bash: go test", "8 results",
		"not explained",
		"THE 2 TOOL RESULTS THAT COST THE MOST TO CARRY", "Read: main.go",
		"REPEATS", "3 reads of a file", "2 commands with the same output",
		"WHAT COULD HAVE BEEN SAVED", "cap at", "(7 results cut)", "every  100 requests", "(1 restart)", "A negative number",
		"HOW FAR TO TRUST THIS", "within 1%", "97%",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the report lacks %q:\n%s", want, out)
		}
	}
	// Only three kinds are listed under a group, and only if they are worth a line.
	if strings.Contains(out, "Bash: ls  ") && strings.Count(out, "Bash: ls") > 2 {
		t.Errorf("more than three kinds listed under tool calls:\n%s", out)
	}
	if strings.Contains(out, "Bash: tiny") {
		t.Errorf("a kind under half a percent must not be listed:\n%s", out)
	}
}

func TestAnEmptyAnalysisWritesLittle(t *testing.T) {
	out := render(func(b *bytes.Buffer) { Session(b, &analyze.Analysis{}) })
	for _, unwanted := range []string{"CONTEXT", "WHERE THE INPUT", "REPEATS", "WHAT COULD", "THE 0 TOOL"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("an empty analysis wrote %q:\n%s", unwanted, out)
		}
	}
	if !strings.Contains(out, "COST  $0.00") {
		t.Errorf("out:\n%s", out)
	}
}

func TestEstimatedCostSaysSo(t *testing.T) {
	a := fullAnalysis()
	a.Cost.Source = analyze.SourceEstimated
	a.Cost.Check = nil
	a.Cost.Unknown = nil
	a.Cost.Unsplit = 0
	a.Cost.SubagentUSD = 0
	a.Session.LinesSkipped, a.Session.Truncated = 0, false
	a.Session.Started = time.Time{}
	out := render(func(b *bytes.Buffer) { Session(b, a) })
	if !strings.Contains(out, "(estimated from the tokens of each request)") || strings.Contains(out, "check:") || strings.Contains(out, "note:") {
		t.Errorf("out:\n%s", out)
	}
	if strings.Contains(out, "started") {
		t.Errorf("a session without times must not print them:\n%s", out)
	}
}

func TestAnUnverifiedCheckThatAgreesIsNotMentioned(t *testing.T) {
	a := fullAnalysis()
	a.Cost.Check = []analyze.Check{{Verified: false, DiffPercent: 0}}
	out := render(func(b *bytes.Buffer) { Session(b, a) })
	if strings.Contains(out, "unverified") {
		t.Errorf("out:\n%s", out)
	}
}

func TestManyShowsATableAndTheTotal(t *testing.T) {
	a, b := fullAnalysis(), fullAnalysis()
	b.Session.File = "99999999.jsonl"
	b.Session.Started = time.Time{}
	b.Cost.Source = analyze.SourceEstimated
	as := []*analyze.Analysis{a, b}
	s := analyze.Combine(as, 5)
	out := render(func(w *bytes.Buffer) { Many(w, as, s) })
	for _, want := range []string{
		"2 sessions, 240 requests", "abcdef12", "99999999", "  -  ", "all",
		"ALL OF THEM TOGETHER", "1 of 2 sessions recorded by Claude Code, the rest estimated",
		"WHERE THE INPUT COST CAME FROM", "WHAT COULD HAVE BEEN SAVED", "reproduced it exactly",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	// When every session has a recorded cost, the line says only that.
	s2 := analyze.Combine([]*analyze.Analysis{a}, 5)
	out = render(func(w *bytes.Buffer) { Many(w, []*analyze.Analysis{a}, s2) })
	if !strings.Contains(out, "(recorded by Claude Code)") {
		t.Errorf("out:\n%s", out)
	}
}

func TestJSON(t *testing.T) {
	var b bytes.Buffer
	if err := JSON(&b, fullAnalysis()); err != nil {
		t.Fatal(err)
	}
	var back analyze.Analysis
	if err := json.Unmarshal(b.Bytes(), &back); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if back.Session.Requests != 120 || back.Cost.TotalUSD != 12 || len(back.Carry.Slices) != 8 {
		t.Errorf("round trip lost data: %+v", back.Session)
	}
	if !strings.Contains(b.String(), "\n  \"session\"") {
		t.Error("the JSON should be indented")
	}
}
