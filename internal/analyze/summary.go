package analyze

import (
	"math"
	"sort"

	"github.com/sungwoonpk/leanfox/internal/pricing"
)

// Summary is several sessions added together.
type Summary struct {
	Sessions      int          `json:"sessions"`
	Requests      int          `json:"requests"`
	TotalUSD      float64      `json:"total_usd"`
	Parts         pricing.Cost `json:"parts"`
	Recorded      int          `json:"sessions_with_recorded_cost"`
	InputSideUSD  float64      `json:"input_side_usd"`
	Slices        []Slice      `json:"slices"`
	Carriers      []Carrier    `json:"carriers"`
	Rebuilds      int          `json:"cache_rebuilds"`
	RebuildUSD    float64      `json:"cache_rebuild_usd"`
	Unexplained   float64      `json:"unexplained_usd"`
	Repeats       Repeats      `json:"repeats"`
	WhatIf        WhatIf       `json:"what_if"`
	FitError      float64      `json:"fit_error"`
	WorstCheckPct float64      `json:"worst_check_percent"`
	Unknown       []string     `json:"unknown_models,omitempty"`
}

// Combine adds the analyses of several sessions. The shares are worked out again against the combined cost, and the
// largest carriers of all sessions are listed together.
func Combine(as []*Analysis, top int) *Summary {
	if top <= 0 {
		top = 10
	}
	s := &Summary{Sessions: len(as)}
	slices := map[[2]string]*Slice{}
	rows := map[string]*RepeatRow{}
	unknown := map[string]bool{}
	var carriers []Carrier
	var fitWeighted, fitRequests float64

	for _, a := range as {
		s.Requests += a.Session.Requests
		s.TotalUSD += a.Cost.TotalUSD
		s.Parts = s.Parts.Add(a.Cost.Parts)
		if a.Cost.Source == SourceRecorded {
			s.Recorded++
		}
		for _, m := range a.Cost.Unknown {
			unknown[m] = true
		}
		for _, c := range a.Cost.Check {
			if c.Verified && math.Abs(c.DiffPercent) > math.Abs(s.WorstCheckPct) {
				s.WorstCheckPct = c.DiffPercent
			}
		}
		s.InputSideUSD += a.Carry.InputSideUSD
		s.Rebuilds += a.Carry.Rebuilds
		s.RebuildUSD += a.Carry.RebuildUSD
		s.Unexplained += a.Carry.Unexplained
		fitWeighted += a.Carry.FitError * float64(a.Session.Requests)
		fitRequests += float64(a.Session.Requests)

		for _, sl := range a.Carry.Slices {
			m := slice(slices, sl.Group, sl.Name)
			m.Items += sl.Items
			m.Tokens += sl.Tokens
			m.CarryUSD += sl.CarryUSD
		}
		for _, c := range a.Carriers {
			c.Session = a.Session.File
			carriers = append(carriers, c)
		}
		addTotal(&s.Repeats.Rereads, a.Repeats.Rereads)
		addTotal(&s.Repeats.SameOutputs, a.Repeats.SameOutputs)
		for _, r := range a.Repeats.Top {
			m := rows[r.What]
			if m == nil {
				m = &RepeatRow{What: r.What}
				rows[r.What] = m
			}
			m.Times += r.Times
			m.Tokens += r.Tokens
			m.USD += r.USD
		}
		addWhatIf(&s.WhatIf, a.WhatIf)
	}

	if fitRequests > 0 {
		s.FitError = fitWeighted / fitRequests
	}
	for _, sl := range slices {
		if s.InputSideUSD > 0 {
			sl.Share = sl.CarryUSD / s.InputSideUSD
		}
		s.Slices = append(s.Slices, *sl)
	}
	sort.Slice(s.Slices, func(i, j int) bool { return s.Slices[i].Name < s.Slices[j].Name })
	sort.SliceStable(s.Slices, func(i, j int) bool { return s.Slices[i].CarryUSD > s.Slices[j].CarryUSD })

	sort.SliceStable(carriers, func(i, j int) bool { return carriers[i].CarryUSD > carriers[j].CarryUSD })
	if len(carriers) > top {
		carriers = carriers[:top]
	}
	for i := range carriers {
		if s.InputSideUSD > 0 {
			carriers[i].Share = carriers[i].CarryUSD / s.InputSideUSD
		}
	}
	s.Carriers = carriers

	for _, r := range rows {
		s.Repeats.Top = append(s.Repeats.Top, *r)
	}
	sort.Slice(s.Repeats.Top, func(i, j int) bool { return s.Repeats.Top[i].What < s.Repeats.Top[j].What })
	sort.SliceStable(s.Repeats.Top, func(i, j int) bool { return s.Repeats.Top[i].USD > s.Repeats.Top[j].USD })
	if len(s.Repeats.Top) > 5 {
		s.Repeats.Top = s.Repeats.Top[:5]
	}
	s.Unknown = sortedKeys(unknown)
	return s
}

func addTotal(dst *RepeatTotal, src RepeatTotal) {
	dst.Count += src.Count
	dst.Tokens += src.Tokens
	dst.USD += src.USD
}

func addWhatIf(dst *WhatIf, src WhatIf) {
	if dst.HandoffTokens == 0 {
		dst.HandoffTokens = src.HandoffTokens
	}
	if dst.Cap == nil {
		dst.Cap = make([]CapOutcome, len(src.Cap))
		for i, c := range src.Cap {
			dst.Cap[i].LimitTokens = c.LimitTokens
		}
	}
	if dst.Split == nil {
		dst.Split = make([]SplitOutcome, len(src.Split))
		for i, c := range src.Split {
			dst.Split[i].EveryRequests = c.EveryRequests
		}
	}
	for i, c := range src.Cap {
		dst.Cap[i].Affected += c.Affected
		dst.Cap[i].ElidedTokens += c.ElidedTokens
		dst.Cap[i].SavedUSD += c.SavedUSD
	}
	for i, c := range src.Split {
		dst.Split[i].Restarts += c.Restarts
		dst.Split[i].SavedUSD += c.SavedUSD
	}
}
