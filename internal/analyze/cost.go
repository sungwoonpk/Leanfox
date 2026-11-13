package analyze

import (
	"math"
	"sort"

	"github.com/sungwoonpk/leanfox/internal/pricing"
	"github.com/sungwoonpk/leanfox/internal/transcript"
)

// Cost sources.
const (
	SourceRecorded  = "recorded"
	SourceEstimated = "estimated"
)

// CostReport is what the session cost, and how sure that number is.
type CostReport struct {
	// Source is "recorded" when Claude Code wrote its own total into the file, and "estimated" when the total was
	// computed here from the tokens of each request.
	Source   string       `json:"source"`
	TotalUSD float64      `json:"total_usd"`
	Parts    pricing.Cost `json:"parts"`
	// Unsplit is the part of the total that could not be split into parts, because the model is not in the price table.
	Unsplit float64    `json:"unsplit_usd"`
	Models  []ModelRow `json:"models"`
	// Check compares each recorded model cost with the cost computed from the same recorded tokens.
	Check   []Check  `json:"check,omitempty"`
	Unknown []string `json:"unknown_models,omitempty"`
	// Coverage is how much of the recorded cache-read volume the requests in the file account for. Under 1 means the
	// file lacks some requests, for example those of an earlier run that was resumed.
	Coverage    float64 `json:"coverage,omitempty"`
	SubagentUSD float64 `json:"subagent_usd"`
}

// ModelRow is the usage of one model, as the requests in the file show it.
type ModelRow struct {
	Model    string         `json:"model"`
	Requests int            `json:"requests"`
	Tokens   pricing.Tokens `json:"tokens"`
	USD      float64        `json:"usd"`
	Known    bool           `json:"known"`
}

// Check is one comparison of a recorded cost with the computed one.
type Check struct {
	Model       string  `json:"model"`
	RecordedUSD float64 `json:"recorded_usd"`
	ComputedUSD float64 `json:"computed_usd"`
	// DiffPercent is positive when the computed cost is the larger.
	DiffPercent float64 `json:"diff_percent"`
	Verified    bool    `json:"verified"`
}

func costReport(a *Analysis, s *transcript.Session, all []turn, tab *pricing.Table) {
	r := &a.Cost
	rows := map[string]*ModelRow{}
	for _, t := range all {
		row := rows[t.model]
		if row == nil {
			row = &ModelRow{Model: t.model}
			rows[t.model] = row
		}
		row.Requests++
		row.Tokens = row.Tokens.Add(t.tokens)
		if e, ok := tab.Lookup(t.model); ok {
			row.Known = true
			c := e.Cost(t.tokens).Total()
			row.USD += c
			if t.side {
				r.SubagentUSD += c
			}
		}
	}
	unknown := map[string]bool{}
	for _, row := range rows {
		r.Models = append(r.Models, *row)
		if !row.Known {
			unknown[row.Model] = true
		}
	}
	sort.Slice(r.Models, func(i, j int) bool {
		if r.Models[i].USD != r.Models[j].USD {
			return r.Models[i].USD > r.Models[j].USD
		}
		return r.Models[i].Model < r.Models[j].Model
	})

	if len(s.Costs) == 0 {
		r.Source = SourceEstimated
		for _, row := range r.Models {
			if e, ok := tab.Lookup(row.Model); ok {
				r.Parts = r.Parts.Add(e.Cost(row.Tokens))
			}
		}
		r.TotalUSD = r.Parts.Total()
		r.Unknown = sortedKeys(unknown)
		return
	}

	// The last cost state is the final total.
	last := s.Costs[len(s.Costs)-1]
	r.Source = SourceRecorded
	r.TotalUSD = last.TotalUSD
	var recordedRead int64
	for model, mc := range last.Models {
		recordedRead += mc.Tokens.CacheRead
		e, ok := tab.Lookup(model)
		if !ok {
			unknown[model] = true
			continue
		}
		c := e.Cost(mc.Tokens)
		r.Parts = r.Parts.Add(c)
		chk := Check{Model: model, RecordedUSD: mc.USD, ComputedUSD: c.Total(), Verified: e.Verified}
		if mc.USD > 0 {
			chk.DiffPercent = (c.Total() - mc.USD) / mc.USD * 100
		}
		r.Check = append(r.Check, chk)
	}
	sort.Slice(r.Check, func(i, j int) bool { return r.Check[i].Model < r.Check[j].Model })
	r.Unsplit = math.Max(0, r.TotalUSD-r.Parts.Total())
	r.Unknown = sortedKeys(unknown)
	if recordedRead > 0 {
		var seen int64
		for _, row := range r.Models {
			seen += row.Tokens.CacheRead
		}
		r.Coverage = float64(seen) / float64(recordedRead)
	}
}

func sortedKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
