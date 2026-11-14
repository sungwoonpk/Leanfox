package analyze

import "math"

// WhatIf holds two simulations on the measured session. Both are ceilings: they assume that nothing else changes, the
// agent takes the same steps, and what was taken out is never needed again. They say what is worth trying, not what
// will be saved.
type WhatIf struct {
	// Cap is the saving if every tool result bigger than a limit had been cut down to it.
	Cap []CapOutcome `json:"cap_results"`
	// Split is the saving if the conversation had been restarted every so many requests from a short hand-off note.
	Split         []SplitOutcome `json:"split_session"`
	HandoffTokens int64          `json:"handoff_tokens"`
}

// CapOutcome is one limit of the cap simulation.
type CapOutcome struct {
	LimitTokens  int64   `json:"limit_tokens"`
	Affected     int     `json:"results_affected"`
	ElidedTokens int64   `json:"tokens_elided"`
	SavedUSD     float64 `json:"saved_usd"`
}

// SplitOutcome is one interval of the split simulation.
type SplitOutcome struct {
	EveryRequests int     `json:"every_requests"`
	Restarts      int     `json:"restarts"`
	SavedUSD      float64 `json:"saved_usd"`
}

var (
	capLimits  = []int64{500, 1000, 2000, 4000, 8000}
	splitEvery = []int{50, 100, 200, 400}
)

func whatIf(a *Analysis, segs []*segment, u unit, opts Options) {
	w := &a.WhatIf
	w.HandoffTokens = opts.HandoffTokens

	for _, limit := range capLimits {
		o := CapOutcome{LimitTokens: limit}
		var elided float64
		for _, sg := range segs {
			for i := range sg.items {
				it := &sg.items[i]
				if it.group != GroupToolResults || it.at < 1 || it.tokens <= float64(limit) {
					continue
				}
				cut := it.tokens - float64(limit)
				o.Affected++
				elided += cut
				o.SavedUSD += cut * (u.write + float64(it.carried-1)*u.read)
			}
		}
		o.ElidedTokens = int64(math.Round(elided))
		w.Cap = append(w.Cap, o)
	}

	for _, every := range splitEvery {
		o := SplitOutcome{EveryRequests: every}
		for _, sg := range segs {
			saved, restarts := splitSaving(sg.ctx, every, opts.HandoffTokens, u)
			o.SavedUSD += saved
			o.Restarts += restarts
		}
		w.Split = append(w.Split, o)
	}
}

// splitSaving prices a restart every `every` requests. A new piece starts from the base of the segment plus a hand-off
// note, writes that to the cache once, and then grows the way the original did, minus everything before the restart.
// A piece whose context would be larger than the original (early in a session) counts as a loss.
func splitSaving(ctx []int64, every int, handoff int64, u unit) (saved float64, restarts int) {
	if every <= 0 || len(ctx) <= every {
		return 0, 0
	}
	base := ctx[0] + handoff
	var start int
	for k := range ctx {
		if k > 0 && k%every == 0 {
			start = k
			restarts++
			saved -= float64(base) * (u.write - u.read) // the first request of a piece writes its base, it does not read it
		}
		if start == 0 {
			continue
		}
		fresh := math.Max(0, float64(base)+float64(ctx[k]-ctx[start]))
		saved += (float64(ctx[k]) - fresh) * u.read
	}
	return saved, restarts
}
