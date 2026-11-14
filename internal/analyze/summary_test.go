package analyze

import (
	"testing"

	"github.com/sungwoonpk/leanfox/internal/pricing"
)

func twoSessions() []*Analysis {
	a := &Analysis{
		Session: SessionInfo{File: "a.jsonl", Requests: 10},
		Cost: CostReport{
			Source: SourceRecorded, TotalUSD: 10, Parts: pricing.Cost{CacheRead: 6, Output: 2},
			Check:   []Check{{Verified: true, DiffPercent: -0.5}, {Verified: false, DiffPercent: 9}},
			Unknown: []string{"zeta"},
		},
		Carry: CarryReport{
			InputSideUSD: 8, FitError: 0.02, Rebuilds: 1, RebuildUSD: 0.5, Unexplained: 0.1,
			Slices: []Slice{
				{Group: GroupToolResults, Name: "Bash: git status", Items: 2, Tokens: 100, CarryUSD: 2},
				{Group: GroupToolCalls, Name: "Write", Items: 1, Tokens: 50, CarryUSD: 1},
			},
		},
		Carriers: []Carrier{{Tool: "Bash", What: "Bash: git status", CarryUSD: 1}, {Tool: "Read", What: "Read", CarryUSD: 0.2}},
		Repeats: Repeats{
			Rereads:  RepeatTotal{Count: 1, Tokens: 10, USD: 0.1},
			Rewrites: RepeatTotal{Count: 1, Tokens: 100, USD: 0.5},
			Top:      []RepeatRow{{What: "Read: a.go", Times: 1, Tokens: 10, USD: 0.1}},
		},
		WhatIf: WhatIf{
			HandoffTokens: 3000,
			Cap:           []CapOutcome{{LimitTokens: 500, Affected: 1, ElidedTokens: 10, SavedUSD: 1}},
			Split:         []SplitOutcome{{EveryRequests: 50, Restarts: 1, SavedUSD: 2}},
		},
	}
	b := &Analysis{
		Session: SessionInfo{File: "b.jsonl", Requests: 30},
		Cost: CostReport{
			Source: SourceEstimated, TotalUSD: 20, Parts: pricing.Cost{CacheRead: 12, Output: 4},
			Check:   []Check{{Verified: true, DiffPercent: 0.9}},
			Unknown: []string{"alpha", "zeta"},
		},
		Carry: CarryReport{
			InputSideUSD: 12, FitError: 0.06, Rebuilds: 2, RebuildUSD: 1, Unexplained: -0.2,
			Slices: []Slice{{Group: GroupToolResults, Name: "Bash: git status", Items: 3, Tokens: 300, CarryUSD: 6}},
		},
		Carriers: []Carrier{{Tool: "Bash", What: "Bash: go test", CarryUSD: 3}},
		Repeats: Repeats{
			Rereads:     RepeatTotal{Count: 2, Tokens: 20, USD: 0.3},
			SameOutputs: RepeatTotal{Count: 1, Tokens: 5, USD: 0.05},
			Rewrites:    RepeatTotal{Count: 2, Tokens: 300, USD: 1.5},
			Top:         []RepeatRow{{What: "Read: a.go", Times: 2, Tokens: 20, USD: 0.3}, {What: "Bash: git status", Times: 1, Tokens: 5, USD: 0.05}},
		},
		WhatIf: WhatIf{
			HandoffTokens: 3000,
			Cap:           []CapOutcome{{LimitTokens: 500, Affected: 4, ElidedTokens: 40, SavedUSD: 3}},
			Split:         []SplitOutcome{{EveryRequests: 50, Restarts: 3, SavedUSD: 5}},
		},
	}
	return []*Analysis{a, b}
}

func TestCombineAddsTheSessionsUp(t *testing.T) {
	s := Combine(twoSessions(), 2)
	if s.Sessions != 2 || s.Requests != 40 || s.Recorded != 1 {
		t.Errorf("sessions %d requests %d recorded %d", s.Sessions, s.Requests, s.Recorded)
	}
	near(t, "total", s.TotalUSD, 30, 1e-9)
	near(t, "cache read part", s.Parts.CacheRead, 18, 1e-9)
	near(t, "input side", s.InputSideUSD, 20, 1e-9)
	if s.Rebuilds != 3 {
		t.Errorf("rebuilds = %d", s.Rebuilds)
	}
	near(t, "rebuild cost", s.RebuildUSD, 1.5, 1e-9)
	near(t, "unexplained", s.Unexplained, -0.1, 1e-9)
	// Weighted by requests: (0.02 x 10 + 0.06 x 30) / 40.
	near(t, "fit error", s.FitError, 0.05, 1e-9)
	// The unverified model's 9 percent is not counted, the verified 0.9 is the largest.
	near(t, "worst check", s.WorstCheckPct, 0.9, 1e-9)
	if len(s.Unknown) != 2 || s.Unknown[0] != "alpha" || s.Unknown[1] != "zeta" {
		t.Errorf("unknown = %v", s.Unknown)
	}
}

func TestCombineMergesSlicesAndRecomputesShares(t *testing.T) {
	s := Combine(twoSessions(), 10)
	if len(s.Slices) != 2 {
		t.Fatalf("slices = %+v", s.Slices)
	}
	top := s.Slices[0]
	if top.Name != "Bash: git status" || top.Items != 5 || top.Tokens != 400 {
		t.Errorf("top slice = %+v", top)
	}
	near(t, "carry", top.CarryUSD, 8, 1e-9)
	near(t, "share of the combined input side", top.Share, 8.0/20.0, 1e-9)
}

func TestCombineListsTheLargestCarriersOfAllSessions(t *testing.T) {
	s := Combine(twoSessions(), 2)
	if len(s.Carriers) != 2 {
		t.Fatalf("carriers = %+v", s.Carriers)
	}
	if s.Carriers[0].What != "Bash: go test" || s.Carriers[0].Session != "b.jsonl" || s.Carriers[1].Session != "a.jsonl" {
		t.Errorf("carriers = %+v", s.Carriers)
	}
	near(t, "share", s.Carriers[0].Share, 3.0/20.0, 1e-9)
}

func TestCombineMergesRepeatsAndWhatIfs(t *testing.T) {
	s := Combine(twoSessions(), 10)
	if s.Repeats.Rereads.Count != 3 || s.Repeats.Rereads.Tokens != 30 || s.Repeats.SameOutputs.Count != 1 {
		t.Errorf("repeats = %+v", s.Repeats)
	}
	if w := s.Repeats.Rewrites; w.Count != 3 || w.Tokens != 400 || w.USD != 2 {
		t.Errorf("rewrites = %+v", w)
	}
	if len(s.Repeats.Top) != 2 || s.Repeats.Top[0].What != "Read: a.go" || s.Repeats.Top[0].Times != 3 {
		t.Errorf("top = %+v", s.Repeats.Top)
	}
	if s.WhatIf.HandoffTokens != 3000 || len(s.WhatIf.Cap) != 1 || len(s.WhatIf.Split) != 1 {
		t.Fatalf("what if = %+v", s.WhatIf)
	}
	if c := s.WhatIf.Cap[0]; c.LimitTokens != 500 || c.Affected != 5 || c.ElidedTokens != 50 || c.SavedUSD != 4 {
		t.Errorf("cap = %+v", c)
	}
	if c := s.WhatIf.Split[0]; c.EveryRequests != 50 || c.Restarts != 4 || c.SavedUSD != 7 {
		t.Errorf("split = %+v", c)
	}
}

func TestOnlyTheFiveBiggestRepeatsOfAllSessionsAreKept(t *testing.T) {
	a := &Analysis{}
	for i, name := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		a.Repeats.Top = append(a.Repeats.Top, RepeatRow{What: name, USD: float64(i)})
	}
	s := Combine([]*Analysis{a}, 3)
	if len(s.Repeats.Top) != 5 || s.Repeats.Top[0].What != "g" {
		t.Errorf("top = %+v", s.Repeats.Top)
	}
}

func TestCombineOfNothing(t *testing.T) {
	s := Combine(nil, 0)
	if s.Sessions != 0 || s.TotalUSD != 0 || s.FitError != 0 || len(s.Slices) != 0 || len(s.Carriers) != 0 {
		t.Errorf("summary = %+v", s)
	}
	// No input-side cost: the shares stay zero and nothing divides by zero.
	s = Combine([]*Analysis{{Carry: CarryReport{Slices: []Slice{{Group: GroupBase, Name: "x", CarryUSD: 1}}}, Carriers: []Carrier{{CarryUSD: 1}}}}, 5)
	if s.Slices[0].Share != 0 || s.Carriers[0].Share != 0 {
		t.Errorf("summary = %+v", s)
	}
}
