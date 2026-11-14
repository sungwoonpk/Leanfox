package analyze

import (
	"math"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/sungwoonpk/leanfox/internal/pricing"
	"github.com/sungwoonpk/leanfox/internal/transcript"
)

// builder makes a session out of events, so a test can say exactly what happened.
type builder struct{ s *transcript.Session }

func newSession() *builder {
	return &builder{s: &transcript.Session{Path: "/some/dir/s.jsonl", ID: "s1"}}
}

func (b *builder) add(e transcript.Event) *builder {
	b.s.Events = append(b.s.Events, e)
	return b
}

// turn adds a request to the main conversation. cw is written to the one-hour cache.
func (b *builder) turn(model string, in, out, cr, cw int64) *builder {
	return b.add(transcript.Event{
		Kind: transcript.KindTurn, RequestID: "r", Model: model,
		Tokens: pricing.Tokens{Input: in, Output: out, CacheRead: cr, CacheWrite1h: cw},
	})
}

func (b *builder) side(model string, in, out, cr, cw int64) *builder {
	b.turn(model, in, out, cr, cw)
	b.s.Events[len(b.s.Events)-1].Sidechain = true
	return b
}

func (b *builder) text(kind transcript.Kind, chars int) *builder {
	return b.add(transcript.Event{Kind: kind, Chars: chars})
}

func (b *builder) use(id, tool, detail string, chars int) *builder {
	return b.add(transcript.Event{Kind: transcript.KindToolUse, ToolID: id, Tool: tool, Detail: detail, Chars: chars})
}

func (b *builder) result(id, tool string, chars int, digest uint64) *builder {
	return b.add(transcript.Event{Kind: transcript.KindToolResult, ToolID: id, Tool: tool, Chars: chars, Digest: digest})
}

func (b *builder) cost(total float64, models map[string]transcript.ModelCost) *builder {
	b.s.Costs = append(b.s.Costs, transcript.CostState{TotalUSD: total, Models: models})
	return b
}

const sonnet = "claude-sonnet-5-5"

func near(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %v, want %v (tolerance %v)", name, got, want, tol)
	}
}

func TestRecordedCostIsCheckedAgainstTheComputedOne(t *testing.T) {
	b := newSession().
		turn(sonnet, 0, 1000, 0, 100_000).
		turn(sonnet, 10, 500, 100_000, 2_000)
	// By hand: output 1500 x $10, cache write 102000 x $4, cache read 100000 x $0.10, input 10 x $2, per million.
	b.cost(0.43302, map[string]transcript.ModelCost{sonnet: {
		Tokens: pricing.Tokens{Input: 10, Output: 1500, CacheRead: 100_000, CacheWrite1h: 102_000}, USD: 0.43302,
	}})
	a := Analyze(b.s, Options{})
	c := a.Cost
	if c.Source != SourceRecorded {
		t.Errorf("source = %s", c.Source)
	}
	near(t, "total", c.TotalUSD, 0.43302, 1e-9)
	near(t, "parts total", c.Parts.Total(), 0.43302, 1e-9)
	near(t, "output part", c.Parts.Output, 0.015, 1e-9)
	near(t, "cache write part", c.Parts.CacheWrite, 0.408, 1e-9)
	near(t, "cache read part", c.Parts.CacheRead, 0.01, 1e-9)
	near(t, "unsplit", c.Unsplit, 0, 1e-9)
	if len(c.Check) != 1 || !c.Check[0].Verified {
		t.Fatalf("check = %+v", c.Check)
	}
	near(t, "difference", c.Check[0].DiffPercent, 0, 1e-6)
	near(t, "coverage", c.Coverage, 1, 1e-9)
	if len(c.Models) != 1 || c.Models[0].Requests != 2 || !c.Models[0].Known {
		t.Errorf("models = %+v", c.Models)
	}
}

func TestADifferenceBetweenRecordedAndComputedShows(t *testing.T) {
	b := newSession().turn(sonnet, 0, 1_000_000, 0, 0).
		cost(11, map[string]transcript.ModelCost{sonnet: {Tokens: pricing.Tokens{Output: 1_000_000}, USD: 11}})
	a := Analyze(b.s, Options{})
	// $10 computed against $11 recorded.
	near(t, "difference", a.Cost.Check[0].DiffPercent, -100.0/11, 1e-9)
}

func TestWithoutARecordedCostTheCostIsEstimated(t *testing.T) {
	b := newSession().turn(sonnet, 0, 1_000_000, 0, 0)
	a := Analyze(b.s, Options{})
	if a.Cost.Source != SourceEstimated {
		t.Errorf("source = %s", a.Cost.Source)
	}
	near(t, "total", a.Cost.TotalUSD, 10, 1e-9)
	if len(a.Cost.Check) != 0 {
		t.Errorf("an estimate has nothing to check against: %+v", a.Cost.Check)
	}
}

func TestAModelNotInTheTableIsFlaggedAndNeverPriced(t *testing.T) {
	// Estimated: the unknown model adds nothing and is listed.
	b := newSession().turn(sonnet, 0, 1_000_000, 0, 0).turn("mystery-1", 0, 1_000_000, 0, 0)
	a := Analyze(b.s, Options{})
	near(t, "estimated total", a.Cost.TotalUSD, 10, 1e-9)
	if len(a.Cost.Unknown) != 1 || a.Cost.Unknown[0] != "mystery-1" {
		t.Errorf("unknown = %v", a.Cost.Unknown)
	}

	// Recorded: the total stands, and the part that could not be split is shown.
	b = newSession().turn(sonnet, 0, 1_000_000, 0, 0).turn("mystery-1", 0, 1, 0, 0).cost(15, map[string]transcript.ModelCost{
		sonnet:      {Tokens: pricing.Tokens{Output: 1_000_000}, USD: 10},
		"mystery-1": {Tokens: pricing.Tokens{Output: 1}, USD: 5},
	})
	a = Analyze(b.s, Options{})
	near(t, "recorded total", a.Cost.TotalUSD, 15, 1e-9)
	near(t, "unsplit", a.Cost.Unsplit, 5, 1e-9)
	if len(a.Cost.Unknown) != 1 || len(a.Cost.Check) != 1 {
		t.Errorf("unknown %v check %+v", a.Cost.Unknown, a.Cost.Check)
	}
}

func TestRatesCanBeOverridden(t *testing.T) {
	b := newSession().turn("mystery-1", 0, 1_000_000, 0, 0)
	tab := pricing.NewTable(map[string]pricing.Rates{"mystery-1": {Output: 3}})
	a := Analyze(b.s, Options{Table: tab})
	near(t, "total", a.Cost.TotalUSD, 3, 1e-9)
}

func TestSubagentsAreCostedButAreNotTheConversation(t *testing.T) {
	b := newSession().
		turn(sonnet, 0, 0, 0, 50_000).
		side(sonnet, 0, 1_000_000, 0, 0).
		turn(sonnet, 0, 0, 50_000, 100)
	a := Analyze(b.s, Options{})
	if a.Session.Requests != 2 || a.Session.SubagentCalls != 1 {
		t.Errorf("requests %d, sub-agent requests %d", a.Session.Requests, a.Session.SubagentCalls)
	}
	near(t, "sub-agent cost", a.Cost.SubagentUSD, 10, 1e-9)
	if a.Context.FinalTokens != 50_100 {
		t.Errorf("final context = %d, a sub-agent's context must not count", a.Context.FinalTokens)
	}
}

func TestAShrinkingContextStartsANewSegment(t *testing.T) {
	b := newSession().
		turn(sonnet, 0, 0, 0, 100_000).
		turn(sonnet, 0, 0, 100_000, 1_000).
		turn(sonnet, 0, 0, 0, 30_000). // under half of 101,000
		turn(sonnet, 0, 0, 30_000, 500).
		turn(sonnet, 0, 0, 0, 29_000) // a dip, but to more than half of what it was
	a := Analyze(b.s, Options{})
	if a.Session.Segments != 2 {
		t.Errorf("segments = %d, want 2", a.Session.Segments)
	}
	// Small contexts that wobble are not segments.
	b = newSession().turn(sonnet, 0, 0, 0, 10_000).turn(sonnet, 0, 0, 0, 1_000)
	if got := Analyze(b.s, Options{}).Session.Segments; got != 1 {
		t.Errorf("segments = %d, want 1", got)
	}
}

func TestContextInfo(t *testing.T) {
	b := newSession()
	for i := int64(1); i <= 10; i++ {
		b.turn(sonnet, 0, 0, 0, i*1000) // contexts 1000, 2000 ... 10000
	}
	a := Analyze(b.s, Options{})
	c := a.Context
	if c.BaseTokens != 1000 || c.MedianTokens != 5000 || c.P90Tokens != 9000 || c.MaxTokens != 10000 || c.FinalTokens != 10000 {
		t.Errorf("context = %+v", c)
	}
	if c.CacheHitRate != 0 {
		t.Errorf("hit rate = %v, want 0: nothing was read from the cache", c.CacheHitRate)
	}

	b = newSession().turn(sonnet, 0, 0, 0, 1000).turn(sonnet, 0, 0, 1000, 0).turn(sonnet, 0, 0, 1000, 1000)
	a = Analyze(b.s, Options{})
	near(t, "hit rate", a.Context.CacheHitRate, 2000.0/4000.0, 1e-9)
}

func TestAnEmptySessionIsNotAnError(t *testing.T) {
	a := Analyze(newSession().s, Options{})
	if a.Session.Requests != 0 || a.Cost.TotalUSD != 0 || len(a.Carry.Slices) != 0 || a.Context.MaxTokens != 0 {
		t.Errorf("analysis = %+v", a)
	}
	// Only content, no request at all.
	b := newSession().text(transcript.KindUserText, 100)
	a = Analyze(b.s, Options{})
	if a.Session.Requests != 0 || len(a.Carry.Slices) != 0 {
		t.Errorf("analysis = %+v", a)
	}
}

func TestSessionInfo(t *testing.T) {
	b := newSession()
	b.s.Version = "1.2.3"
	b.s.First = time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	b.s.Last = b.s.First.Add(time.Hour)
	b.s.Lines, b.s.BadLines, b.s.Truncated = 40, 2, true
	a := Analyze(b.s, Options{})
	si := a.Session
	if si.File != "s.jsonl" || si.ID != "s1" || si.Version != "1.2.3" || si.LinesRead != 40 || si.LinesSkipped != 2 || !si.Truncated {
		t.Errorf("session info = %+v", si)
	}
	if si.Ended.Sub(si.Started) != time.Hour {
		t.Errorf("times = %v %v", si.Started, si.Ended)
	}
}

// grow builds a session where the context grows by exactly density x characters at every request, one kind of text at a
// time, so the measured rate and the attribution can be checked against known numbers.
func grow(model string, steps int, charsPerStep int, density float64, add func(b *builder, i int)) *builder {
	b := newSession()
	ctx := int64(20_000)
	b.turn(model, 0, 10, 0, ctx)
	for i := 1; i <= steps; i++ {
		add(b, i)
		ctx += int64(density * float64(charsPerStep))
		b.turn(model, 0, 10, ctx-int64(density*float64(charsPerStep)), int64(density*float64(charsPerStep)))
	}
	return b
}

func TestTheMeasuredDensityMatchesTheSession(t *testing.T) {
	b := grow(sonnet, 30, 20_000, 0.5, func(b *builder, i int) {
		id := "t" + string(rune('A'+i))
		b.use(id, "Bash", "go test ./...", 0).result(id, "Bash", 20_000, uint64(i))
	})
	a := Analyze(b.s, Options{})
	var rate float64
	for _, r := range a.Carry.Rates {
		if r.Group == GroupToolResults {
			rate = r.Tokens
		}
	}
	near(t, "tool result density", rate, 0.5, 0.01)
	if a.Carry.FitError > 0.01 {
		t.Errorf("fit error = %v, the data is exact", a.Carry.FitError)
	}
	var found bool
	for _, sl := range a.Carry.Slices {
		if sl.Group == GroupToolResults && sl.Name == "Bash: go test" {
			found = true
			if sl.Items != 30 {
				t.Errorf("items = %d", sl.Items)
			}
			near(t, "tokens", float64(sl.Tokens), 30*10_000, 3000)
		}
	}
	if !found {
		t.Errorf("no slice for the test results: %+v", a.Carry.Slices)
	}
}

func TestCarryCostIsWriteOnceAndReadForEveryLaterRequest(t *testing.T) {
	// 11 requests, one result of 100,000 characters (50,000 tokens) in front of request 1, carried by 10 of them.
	b := newSession().turn(sonnet, 0, 0, 0, 1000)
	b.use("a", "Read", "/x/big.go", 10).result("a", "Read", 100_000, 1)
	b.turn(sonnet, 0, 0, 1000, 50_000)
	for range 9 {
		b.turn(sonnet, 0, 0, 51_000, 0)
	}
	a := Analyze(b.s, Options{})
	if len(a.Carriers) == 0 {
		t.Fatal("no carriers")
	}
	c := a.Carriers[0]
	if c.Tool != "Read" || c.Carried != 10 {
		t.Errorf("carrier = %+v", c)
	}
	// The unit prices are the session's own: all writes were one-hour ($4), all reads $0.10.
	wantTokens := 50_000.0
	near(t, "tokens", float64(c.Tokens), wantTokens, 1500)
	near(t, "carry cost", c.CarryUSD, float64(c.Tokens)*(4e-6+9*0.1e-6), 1e-6)
}

func TestWhatWasThereBeforeTheFirstRequestIsTheBase(t *testing.T) {
	b := newSession().text(transcript.KindUserText, 1000).add(transcript.Event{Kind: transcript.KindAttachment, Chars: 5000})
	b.turn(sonnet, 0, 0, 0, 30_000)
	b.text(transcript.KindAssistantText, 500) // after the last request: nothing ever carries it
	a := Analyze(b.s, Options{})
	if len(a.Carry.Slices) != 1 || a.Carry.Slices[0].Group != GroupBase || a.Carry.Slices[0].Tokens != 30_000 {
		t.Errorf("slices = %+v", a.Carry.Slices)
	}
}

func TestACacheRebuildIsCountedAndPriced(t *testing.T) {
	b := newSession().
		turn(sonnet, 0, 0, 0, 100_000).
		turn(sonnet, 0, 0, 100_000, 500).
		turn(sonnet, 0, 0, 0, 100_500). // the cache expired: everything is written again although nothing was added
		turn(sonnet, 0, 0, 100_500, 500)
	a := Analyze(b.s, Options{})
	if a.Carry.Rebuilds != 1 {
		t.Fatalf("rebuilds = %d", a.Carry.Rebuilds)
	}
	// 100,500 tokens were written that had only to be read: the extra is the difference of the two prices.
	near(t, "rebuild cost", a.Carry.RebuildUSD, 100_500*(4e-6-0.1e-6), 1e-6)
}

func TestAContextThatDipsDoesNotMakeAWriteLookBigger(t *testing.T) {
	// The conversation shrank by 1,000 and then 3,000 tokens were written. Nothing grew, so all 3,000 are the rebuild.
	b := newSession().
		turn(sonnet, 0, 0, 0, 100_000).
		turn(sonnet, 0, 0, 96_000, 3_000) // context 99,000: down by 1,000
	a := Analyze(b.s, Options{})
	if a.Carry.Rebuilds != 1 {
		t.Fatalf("rebuilds = %d", a.Carry.Rebuilds)
	}
	near(t, "rebuild cost", a.Carry.RebuildUSD, 3_000*(4e-6-0.1e-6), 1e-9)
}

func TestTheBaseIsWrittenOnceAndReadByEveryLaterRequest(t *testing.T) {
	// Three requests: the 30,000-token base is written by the first and read by the other two.
	b := newSession().turn(sonnet, 0, 0, 0, 30_000).turn(sonnet, 0, 0, 30_000, 0).turn(sonnet, 0, 0, 30_000, 0)
	a := Analyze(b.s, Options{})
	var base Slice
	for _, sl := range a.Carry.Slices {
		if sl.Group == GroupBase {
			base = sl
		}
	}
	near(t, "base carry", base.CarryUSD, 30_000*(4e-6+2*0.1e-6), 1e-9)
}

func TestABaseThatWasAlreadyCachedIsOnlyRead(t *testing.T) {
	// An earlier session left the system prompt in the cache, so the first request read all 30,000 tokens and wrote none.
	b := newSession().turn(sonnet, 0, 0, 30_000, 1_000).turn(sonnet, 0, 0, 31_000, 0).turn(sonnet, 0, 0, 31_000, 0)
	a := Analyze(b.s, Options{})
	var base Slice
	for _, sl := range a.Carry.Slices {
		if sl.Group == GroupBase {
			base = sl
		}
	}
	// 31,000 tokens in the first request: 1,000 written, 30,000 read, and the base is read by two more requests.
	// The base is what was there before the first request, which is the whole first context.
	wantRead := 31_000.0*3*0.1e-6 - 1_000*0.1e-6 // every request reads it, except the 1,000 that the first one wrote
	wantWrite := 1_000 * 4e-6
	near(t, "base carry", base.CarryUSD, wantRead+wantWrite, 1e-9)
}

func TestSmallWritesAreNotRebuilds(t *testing.T) {
	b := newSession().turn(sonnet, 0, 0, 0, 100_000).turn(sonnet, 0, 0, 100_000, 1_500)
	if got := Analyze(b.s, Options{}).Carry.Rebuilds; got != 0 {
		t.Errorf("rebuilds = %d", got)
	}
}

func TestBashResultsAreNamedByKindOfCommand(t *testing.T) {
	b := newSession().turn(sonnet, 0, 0, 0, 30_000)
	b.use("1", "Bash", "cd /repo && git status --short", 40).result("1", "Bash", 4_000, 1)
	b.use("2", "Bash", "go test ./...", 40).result("2", "Bash", 4_000, 2)
	b.result("zzz", "", 4_000, 3) // a result whose call was never seen
	b.turn(sonnet, 0, 0, 30_000, 5_000)
	b.turn(sonnet, 0, 0, 35_000, 100)
	a := Analyze(b.s, Options{})
	names := map[string]bool{}
	for _, sl := range a.Carry.Slices {
		names[sl.Group+"/"+sl.Name] = true
	}
	for _, want := range []string{
		GroupToolResults + "/Bash: git status", GroupToolResults + "/Bash: go test", GroupToolResults + "/(unknown tool)",
		GroupToolCalls + "/Bash: git status", GroupToolCalls + "/Bash: go test",
	} {
		if !names[want] {
			t.Errorf("missing slice %q in %v", want, names)
		}
	}
}

func TestNoCommandArgumentsReachTheReport(t *testing.T) {
	b := newSession().turn(sonnet, 0, 0, 0, 30_000)
	b.use("1", "Bash", "curl -H 'Authorization: Bearer sk-secret-token' https://private.example/api", 80)
	b.result("1", "Bash", 4_000, 1)
	b.turn(sonnet, 0, 0, 30_000, 5_000)
	b.turn(sonnet, 0, 0, 35_000, 100)
	a := Analyze(b.s, Options{})
	var all strings.Builder
	for _, sl := range a.Carry.Slices {
		all.WriteString(sl.Name + "\n")
	}
	for _, c := range a.Carriers {
		all.WriteString(c.What + "\n")
	}
	for _, r := range a.Repeats.Top {
		all.WriteString(r.What + "\n")
	}
	for _, leak := range []string{"secret", "Bearer", "private.example"} {
		if strings.Contains(all.String(), leak) {
			t.Errorf("the report contains %q:\n%s", leak, all.String())
		}
	}
}

func TestRereadsAndRepeatedOutput(t *testing.T) {
	b := newSession().turn(sonnet, 0, 0, 0, 30_000)
	read := func(id string) {
		b.use(id, "Read", "/repo/src/main.go", 40).result(id, "Read", 4_000, 7)
		b.turn(sonnet, 0, 0, 30_000, 2_000)
	}
	read("r1")
	read("r2") // the same file again, nothing changed it
	b.use("e", "Edit", "/repo/src/main.go", 40).result("e", "Edit", 30, 8)
	read("r3") // after an edit a read is not a repeat
	run := func(id string, chars int) {
		b.use(id, "Bash", "git status", 20).result(id, "Bash", chars, 9)
		b.turn(sonnet, 0, 0, 30_000, 2_000)
	}
	run("g1", 500)
	run("g2", 500) // the same output again
	run("g3", 100) // too short to matter, and a different hash of nothing in particular
	run("g4", 100) // short, so not counted although identical
	b.turn(sonnet, 0, 0, 30_000, 100)
	a := Analyze(b.s, Options{})
	if a.Repeats.Rereads.Count != 1 {
		t.Errorf("rereads = %+v, want 1", a.Repeats.Rereads)
	}
	if a.Repeats.SameOutputs.Count != 1 {
		t.Errorf("same outputs = %+v, want 1", a.Repeats.SameOutputs)
	}
	if len(a.Repeats.Top) != 2 {
		t.Fatalf("top = %+v", a.Repeats.Top)
	}
	var whats []string
	for _, r := range a.Repeats.Top {
		whats = append(whats, r.What)
	}
	joined := strings.Join(whats, "|")
	if !strings.Contains(joined, "Read: main.go") || !strings.Contains(joined, "Bash: git status") || strings.Contains(joined, "/repo") {
		t.Errorf("rows = %v: a file is named by its base name only", whats)
	}
}

func TestTheSameOutputOfDifferentCommandsIsNotARepeat(t *testing.T) {
	b := newSession().turn(sonnet, 0, 0, 0, 30_000)
	b.use("a", "Bash", "git status", 5).result("a", "Bash", 800, 5)
	b.turn(sonnet, 0, 0, 30_000, 1_000)
	b.use("b", "Bash", "go test", 5).result("b", "Bash", 800, 5)
	b.turn(sonnet, 0, 0, 31_000, 1_000)
	b.turn(sonnet, 0, 0, 32_000, 100)
	if got := Analyze(b.s, Options{}).Repeats.SameOutputs.Count; got != 0 {
		t.Errorf("same outputs = %d, want 0", got)
	}
}

func TestACompactionForgetsWhatWasRead(t *testing.T) {
	b := newSession().turn(sonnet, 0, 0, 0, 100_000)
	b.use("r1", "Read", "/a.go", 5).result("r1", "Read", 4_000, 1)
	b.turn(sonnet, 0, 0, 100_000, 2_000)
	b.turn(sonnet, 0, 0, 0, 20_000) // compacted
	b.use("r2", "Read", "/a.go", 5).result("r2", "Read", 4_000, 1)
	b.turn(sonnet, 0, 0, 20_000, 2_000)
	b.turn(sonnet, 0, 0, 22_000, 100)
	if got := Analyze(b.s, Options{}).Repeats.Rereads.Count; got != 0 {
		t.Errorf("rereads = %d: after a compaction the file is no longer in the conversation", got)
	}
}

func TestCapSimulation(t *testing.T) {
	// One result of 30,000 tokens in front of request 1 of 3, so two requests carry it. A cap of 500 saves 29,500
	// tokens: written once, read once more. The data is large so that the weak priors do not matter.
	b := newSession().turn(sonnet, 0, 0, 0, 10_000)
	b.use("a", "Read", "/x", 5).result("a", "Read", 60_000, 1)
	b.turn(sonnet, 0, 0, 10_000, 30_000)
	b.turn(sonnet, 0, 0, 40_000, 0)
	a := Analyze(b.s, Options{})
	var c500, c8000 CapOutcome
	for _, c := range a.WhatIf.Cap {
		switch c.LimitTokens {
		case 500:
			c500 = c
		case 8000:
			c8000 = c
		}
	}
	if c500.Affected != 1 || c8000.Affected != 1 || !(c500.SavedUSD > c8000.SavedUSD && c8000.SavedUSD > 0) {
		t.Errorf("cap outcomes = %+v %+v", c500, c8000)
	}
	near(t, "elided at 8000", float64(c8000.ElidedTokens), 22_000, 900)
	near(t, "elided", float64(c500.ElidedTokens), 29_500, 900)
	near(t, "saved", c500.SavedUSD, float64(c500.ElidedTokens)*(4e-6+1*0.1e-6), 1e-4)
}

func TestSplitSaving(t *testing.T) {
	u := unit{read: 1, write: 2}
	// Contexts of 100 ... 600, a restart every 3 requests, no hand-off note. The new piece starts at 100.
	// Request 3: writes its base of 100 where the original read, a loss of 100 x (2-1); the piece reads 100 where the
	// original read 400: 300 saved. Request 4: 500 against 200, request 5: 600 against 300, 300 each.
	ctx := []int64{100, 200, 300, 400, 500, 600}
	saved, restarts := splitSaving(ctx, 3, 0, u)
	if restarts != 1 || saved != -100+300+300+300 {
		t.Errorf("saved %v restarts %d, want 800 and 1", saved, restarts)
	}
	// A hand-off note makes every piece bigger by its size, for every request of the piece.
	saved, _ = splitSaving(ctx, 3, 50, u)
	if saved != -150+250+250+250 {
		t.Errorf("saved %v, want 600", saved)
	}
	// Too short to split, or a nonsense interval.
	if s, r := splitSaving(ctx, 6, 0, u); s != 0 || r != 0 {
		t.Errorf("a session that is not longer than the interval: %v %d", s, r)
	}
	if s, r := splitSaving(ctx, 0, 0, u); s != 0 || r != 0 {
		t.Errorf("interval 0: %v %d", s, r)
	}
	// An early restart can lose: the piece is bigger than the original was.
	small := []int64{100, 110, 120, 130}
	if s, _ := splitSaving(small, 2, 1000, u); s >= 0 {
		t.Errorf("saved %v, want a loss", s)
	}
}

func TestSplitSimulationUsesTheHandoffSize(t *testing.T) {
	b := newSession().turn(sonnet, 0, 0, 0, 20_000)
	ctx := int64(20_000)
	for range 120 {
		b.turn(sonnet, 0, 0, ctx, 2_000)
		ctx += 2_000
	}
	small := Analyze(b.s, Options{HandoffTokens: 1_000}).WhatIf
	big := Analyze(b.s, Options{HandoffTokens: 20_000}).WhatIf
	if small.HandoffTokens != 1_000 || big.HandoffTokens != 20_000 {
		t.Errorf("handoff sizes %d %d", small.HandoffTokens, big.HandoffTokens)
	}
	if !(small.Split[0].SavedUSD > big.Split[0].SavedUSD) {
		t.Errorf("a bigger hand-off must save less: %v vs %v", small.Split[0].SavedUSD, big.Split[0].SavedUSD)
	}
	if small.Split[0].Restarts != 2 || small.Split[1].Restarts != 1 || small.Split[2].Restarts != 0 {
		t.Errorf("restarts = %+v", small.Split)
	}
}

func TestSessionWithoutCacheUsesTheInputPrice(t *testing.T) {
	b := newSession().turn(sonnet, 30_000, 0, 0, 0)
	b.use("a", "Read", "/x", 5).result("a", "Read", 10_000, 1)
	b.turn(sonnet, 36_000, 0, 0, 0)
	b.turn(sonnet, 36_100, 0, 0, 0)
	a := Analyze(b.s, Options{})
	if len(a.Carriers) != 1 || a.Carriers[0].CarryUSD <= 0 {
		t.Fatalf("carriers = %+v", a.Carriers)
	}
	// Two requests carry it, each at the $2 input price.
	near(t, "carry", a.Carriers[0].CarryUSD, float64(a.Carriers[0].Tokens)*2*2e-6, 1e-4)
}

func TestTopCarriersIsLimitedAndSorted(t *testing.T) {
	b := newSession().turn(sonnet, 0, 0, 0, 30_000)
	for i, chars := range []int{2_000, 9_000, 4_000, 6_000} {
		id := string(rune('a' + i))
		b.use(id, "Read", "/f"+id, 5).result(id, "Read", chars, uint64(i))
		b.turn(sonnet, 0, 0, 30_000, int64(chars)/2)
	}
	b.turn(sonnet, 0, 0, 40_000, 100)
	a := Analyze(b.s, Options{TopCarriers: 2})
	if len(a.Carriers) != 2 || a.Carriers[0].CarryUSD < a.Carriers[1].CarryUSD {
		t.Errorf("carriers = %+v", a.Carriers)
	}
}

func TestPercentileAndMedian(t *testing.T) {
	if percentile(nil, 0.5) != 0 || median(nil) != 0 {
		t.Error("nothing has no percentile")
	}
	if got := percentile([]int64{7}, 0.0); got != 7 {
		t.Errorf("percentile 0 of one = %d", got)
	}
	if got := median([]float64{3, 1, 2}); got != 2 {
		t.Errorf("median = %v", got)
	}
}

// bruteNNLS finds the best non-negative fit by trying every choice of columns, which is only possible for a few of them.
func bruteNNLS(cols [][]float64, y []float64) float64 {
	g := len(cols)
	best := math.Inf(1)
	for mask := 0; mask < 1<<g; mask++ {
		var idx []int
		for j := 0; j < g; j++ {
			if mask&(1<<j) != 0 {
				idx = append(idx, j)
			}
		}
		coef := make([]float64, g)
		if len(idx) > 0 {
			gram := make([][]float64, g)
			rhs := make([]float64, g)
			in := make([]bool, g)
			for _, j := range idx {
				in[j] = true
			}
			for i := range gram {
				gram[i] = make([]float64, g)
				for j := range gram[i] {
					for k := range y {
						gram[i][j] += cols[i][k] * cols[j][k]
					}
				}
				for k := range y {
					rhs[i] += cols[i][k] * y[k]
				}
			}
			z, ok := solveSubset(gram, rhs, in)
			if !ok {
				continue
			}
			feasible := true
			for _, j := range idx {
				if z[j] < 0 {
					feasible = false
				}
			}
			if !feasible {
				continue
			}
			coef = z
		}
		var rss float64
		for k := range y {
			pred := 0.0
			for j := range cols {
				pred += coef[j] * cols[j][k]
			}
			rss += (y[k] - pred) * (y[k] - pred)
		}
		best = math.Min(best, rss)
	}
	return best
}

func TestNNLSIsAsGoodAsTryingEveryChoiceOfColumns(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 9))
	for trial := range 300 {
		g, n := 2+rng.IntN(3), 5+rng.IntN(10)
		cols := make([][]float64, g)
		for j := range cols {
			cols[j] = make([]float64, n)
			for k := range cols[j] {
				cols[j][k] = rng.Float64()*10 - 2 // some negative values, so that a coefficient can want to be negative
			}
		}
		y := make([]float64, n)
		for k := range y {
			y[k] = rng.Float64()*20 - 8
		}
		got := nnls(cols, y)
		var rss float64
		for k := range y {
			pred := 0.0
			for j := range cols {
				if got[j] < 0 {
					t.Fatalf("trial %d: coefficient %d is %v", trial, j, got[j])
				}
				pred += got[j] * cols[j][k]
			}
			rss += (y[k] - pred) * (y[k] - pred)
		}
		want := bruteNNLS(cols, y)
		if rss > want*(1+1e-6)+1e-9 {
			t.Fatalf("trial %d: residual %v, the best possible is %v", trial, rss, want)
		}
	}
}

func TestNNLSStepsBackWhenAnEarlierCoefficientWouldGoNegative(t *testing.T) {
	// Found by search: the third column enters after the first two, and with it in, the first coefficient wants to go
	// below zero, so the solver has to stop part of the way and drop it.
	cols := [][]float64{{0, 3, 4}, {3, 0, 2}, {1, 2, 4}}
	y := []float64{5, 5, 3}
	got := nnls(cols, y)
	var rss float64
	for k := range y {
		pred := 0.0
		for j := range cols {
			if got[j] < 0 {
				t.Fatalf("coefficient %d is %v", j, got[j])
			}
			pred += got[j] * cols[j][k]
		}
		rss += (y[k] - pred) * (y[k] - pred)
	}
	if want := bruteNNLS(cols, y); rss > want*(1+1e-9)+1e-12 {
		t.Errorf("residual %v, the best possible is %v (coefficients %v)", rss, want, got)
	}
}

func TestSolveSubsetReportsASingularSystem(t *testing.T) {
	gram := [][]float64{{1, 1}, {1, 1}}
	if _, ok := solveSubset(gram, []float64{1, 1}, []bool{true, true}); ok {
		t.Error("two identical columns cannot be solved for")
	}
	z, ok := solveSubset([][]float64{{2, 0}, {0, 4}}, []float64{2, 4}, []bool{true, true})
	if !ok || z[0] != 1 || z[1] != 1 {
		t.Errorf("z = %v ok = %v", z, ok)
	}
}

func TestNNLSWithColumnsThatCannotBeToldApart(t *testing.T) {
	a := []float64{1, 2, 3, 4}
	r := nnls([][]float64{a, a, {0, 0, 0, 0}}, []float64{2, 4, 6, 8})
	if math.Abs(r[0]+r[1]-2) > 1e-6 || r[2] != 0 || r[0] < 0 || r[1] < 0 {
		t.Errorf("duplicate columns: %v, want the two to add up to 2 and the empty one to be 0", r)
	}
}

func TestNNLS(t *testing.T) {
	// y = 2*a exactly, and b is noise that must not go negative.
	a := []float64{1, 2, 3, 4}
	bcol := []float64{4, 3, 2, 1}
	y := []float64{2, 4, 6, 8}
	r := nnls([][]float64{a, bcol}, y)
	near(t, "first", r[0], 2, 1e-6)
	if r[1] < 0 || r[1] > 1e-6 {
		t.Errorf("second = %v, want 0", r[1])
	}
	// A column that would want a negative coefficient is held at zero.
	r = nnls([][]float64{a}, []float64{-1, -2, -3, -4})
	if r[0] != 0 {
		t.Errorf("negative case = %v", r[0])
	}
	// A column of zeros, and no data at all.
	r = nnls([][]float64{{0, 0}, {1, 1}}, []float64{3, 3})
	if r[0] != 0 || math.Abs(r[1]-3) > 1e-6 {
		t.Errorf("zero column = %v", r)
	}
	r = nnls([][]float64{{1, 1}, {0, 0}}, []float64{3, 3})
	if r[1] != 0 || math.Abs(r[0]-3) > 1e-6 {
		t.Errorf("zero column last = %v", r)
	}
	if r := nnls([][]float64{{}}, nil); r[0] != 0 {
		t.Errorf("no data = %v", r)
	}
}

func TestOptionsDefaults(t *testing.T) {
	o := Options{}.withDefaults()
	if o.Table == nil || o.TopCarriers != 10 || o.HandoffTokens != 3000 {
		t.Errorf("defaults = %+v", o)
	}
	o = Options{TopCarriers: 3, HandoffTokens: 7}.withDefaults()
	if o.TopCarriers != 3 || o.HandoffTokens != 7 {
		t.Errorf("explicit options changed: %+v", o)
	}
}

func TestGroupIndex(t *testing.T) {
	for i, g := range groups {
		if groupIndex(g) != i {
			t.Errorf("groupIndex(%q) = %d", g, groupIndex(g))
		}
	}
	if groupIndex("nope") != -1 || groupIndex(GroupBase) != -1 {
		t.Error("a name that is not fitted must give -1")
	}
	for _, g := range groups {
		if _, ok := priors[g]; !ok {
			t.Errorf("no prior for %q", g)
		}
	}
}

func TestEveryKindOfTextIsCarriedAndNamed(t *testing.T) {
	// Every kind of text is added in a different amount at each step, so that the kinds can be told apart, and the
	// context grows by exactly the known density of each. The fit must find those densities back.
	dens := map[string]float64{
		GroupUserText: 0.4, GroupAttachments: 0.1, GroupClaudeText: 0.35, GroupToolCalls: 0.5, GroupToolResults: 0.45,
	}
	const think = 950.0 // tokens in one block of reasoning
	rng := rand.New(rand.NewPCG(1, 2))
	b := newSession()
	ctx := int64(30_000)
	b.turn(sonnet, 0, 0, 0, ctx)
	for i := range 80 {
		u, at, c := 200+rng.IntN(4_000), 100+rng.IntN(2_000), 300+rng.IntN(6_000)
		calls, res := 500+rng.IntN(9_000), 400+rng.IntN(12_000)
		b.text(transcript.KindUserText, u)
		b.add(transcript.Event{Kind: transcript.KindAttachment, Chars: at})
		b.text(transcript.KindAssistantText, c)
		b.text(transcript.KindThinking, 10)
		id := string(rune('A' + i))
		b.use(id, "Write", "/x", calls).result(id, "Write", res, uint64(i))
		grown := int64(dens[GroupUserText]*float64(u) + dens[GroupAttachments]*float64(at) + dens[GroupClaudeText]*float64(c) +
			think + dens[GroupToolCalls]*float64(calls) + dens[GroupToolResults]*float64(res))
		b.turn(sonnet, 0, 0, ctx, grown)
		ctx += grown
	}
	a := Analyze(b.s, Options{})
	for _, r := range a.Carry.Rates {
		want := think
		if r.Group != GroupThinking {
			want = dens[r.Group]
		}
		near(t, r.Group+" density", r.Tokens, want, want*0.05)
		unit := "character"
		if r.Group == GroupThinking {
			unit = "block"
		}
		if r.Unit != unit {
			t.Errorf("%s is measured in %q, want %q", r.Group, r.Unit, unit)
		}
	}
	seen := map[string]bool{}
	var sum float64
	for _, sl := range a.Carry.Slices {
		seen[sl.Group] = true
		sum += sl.Share
	}
	for _, g := range append([]string{GroupBase, GroupThinking}, groups[:2]...) {
		if !seen[g] {
			t.Errorf("no slice for %q", g)
		}
	}
	// The base, every kind of text and the rebuilds (there are none) add up to the whole input-side cost.
	near(t, "sum of shares", sum, 1, 0.05)
	if a.Carry.FitError > 0.01 {
		t.Errorf("fit error = %v on exact data", a.Carry.FitError)
	}
}

func TestTheReasoningOfAnotherKindOfEventIsIgnored(t *testing.T) {
	if _, ok := makeItem(&transcript.Event{Kind: transcript.Kind(99)}, map[string]transcript.Event{}); ok {
		t.Error("an event of a kind the analysis does not know must be skipped")
	}
	if _, ok := makeItem(&transcript.Event{Kind: transcript.KindTurn}, map[string]transcript.Event{}); ok {
		t.Error("a request is not a piece of text")
	}
}

func TestAnEventOfAnUnknownKindIsSkippedInASession(t *testing.T) {
	b := newSession().turn(sonnet, 0, 0, 0, 30_000)
	b.add(transcript.Event{Kind: transcript.Kind(99), Chars: 5_000})
	b.turn(sonnet, 0, 0, 30_000, 100)
	a := Analyze(b.s, Options{})
	if len(a.Carry.Slices) != 1 {
		t.Errorf("slices = %+v", a.Carry.Slices)
	}
}

func TestChecksAreOrderedByModel(t *testing.T) {
	b := newSession().turn(sonnet, 0, 10, 0, 0).turn("claude-opus-5-5", 0, 10, 0, 0).cost(1, map[string]transcript.ModelCost{
		sonnet:            {Tokens: pricing.Tokens{Output: 10}, USD: 0.0001},
		"claude-opus-5-5": {Tokens: pricing.Tokens{Output: 10}, USD: 0.0002},
	})
	a := Analyze(b.s, Options{})
	if len(a.Cost.Check) != 2 || a.Cost.Check[0].Model != "claude-opus-5-5" || a.Cost.Check[1].Model != sonnet {
		t.Errorf("checks = %+v", a.Cost.Check)
	}
}

func TestSubagentContentIsLeftOut(t *testing.T) {
	b := newSession().turn(sonnet, 0, 0, 0, 30_000)
	b.add(transcript.Event{Kind: transcript.KindToolResult, ToolID: "x", Tool: "Bash", Chars: 90_000, Sidechain: true})
	b.turn(sonnet, 0, 0, 30_000, 100)
	a := Analyze(b.s, Options{})
	for _, sl := range a.Carry.Slices {
		if sl.Group == GroupToolResults {
			t.Errorf("a sub-agent's result was counted in the conversation: %+v", sl)
		}
	}
}

func TestOnlyTheFiveBiggestRepeatsAreListed(t *testing.T) {
	b := newSession().turn(sonnet, 0, 0, 0, 30_000)
	for i, family := range []string{"git status", "git log", "git diff", "go test", "go vet", "go build", "make test"} {
		for j := range 2 {
			id := family + string(rune('0'+j))
			b.use(id, "Bash", family, 10).result(id, "Bash", 1_000+i*100, uint64(i))
			b.turn(sonnet, 0, 0, 30_000, 1_000)
		}
	}
	b.turn(sonnet, 0, 0, 30_000, 100)
	a := Analyze(b.s, Options{})
	if len(a.Repeats.Top) != 5 || a.Repeats.SameOutputs.Count != 7 {
		t.Errorf("top %d, same outputs %d", len(a.Repeats.Top), a.Repeats.SameOutputs.Count)
	}
	for i := 1; i < len(a.Repeats.Top); i++ {
		if a.Repeats.Top[i-1].USD < a.Repeats.Top[i].USD {
			t.Errorf("not sorted: %+v", a.Repeats.Top)
		}
	}
}

func TestModelsWithTheSameCostAreOrderedByName(t *testing.T) {
	b := newSession().turn("zeta", 0, 1, 0, 0).turn("alpha", 0, 1, 0, 0).turn(sonnet, 0, 1_000_000, 0, 0)
	a := Analyze(b.s, Options{})
	got := []string{a.Cost.Models[0].Model, a.Cost.Models[1].Model, a.Cost.Models[2].Model}
	if got[0] != sonnet || got[1] != "alpha" || got[2] != "zeta" {
		t.Errorf("order = %v", got)
	}
	if len(a.Cost.Unknown) != 2 || a.Cost.Unknown[0] != "alpha" {
		t.Errorf("unknown = %v", a.Cost.Unknown)
	}
}
