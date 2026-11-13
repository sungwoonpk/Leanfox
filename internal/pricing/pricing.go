// Package pricing turns token counts into dollars.
//
// The rates are list prices in US dollars per million tokens. They were copied from Anthropic's published model table
// and then checked against the cost that Claude Code records in its own session files: for the models marked Verified
// below, the cost computed here matched the recorded one to the last digit that is stored (see docs/validation.md).
// A model that is not in the table is never guessed at. The report falls back to the recorded cost and says that it
// cannot split it into parts.
package pricing

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
)

// Rates are dollars per million tokens.
type Rates struct {
	Input        float64 `json:"input"`
	Output       float64 `json:"output"`
	CacheRead    float64 `json:"cache_read"`
	CacheWrite5m float64 `json:"cache_write_5m"`
	CacheWrite1h float64 `json:"cache_write_1h"`
}

// WebSearchDollars is what one server-side web search costs. It does not depend on the model.
const WebSearchDollars = 0.01

// Tokens is what one or more requests used. Output includes thinking tokens, as the API bills it.
type Tokens struct {
	Input        int64
	Output       int64
	CacheRead    int64
	CacheWrite5m int64
	CacheWrite1h int64
	WebSearches  int64
}

// Add returns the sum of two token counts.
func (t Tokens) Add(o Tokens) Tokens {
	return Tokens{
		Input:        t.Input + o.Input,
		Output:       t.Output + o.Output,
		CacheRead:    t.CacheRead + o.CacheRead,
		CacheWrite5m: t.CacheWrite5m + o.CacheWrite5m,
		CacheWrite1h: t.CacheWrite1h + o.CacheWrite1h,
		WebSearches:  t.WebSearches + o.WebSearches,
	}
}

// Context is the number of tokens the model read for this request, whether or not they came from the cache.
func (t Tokens) Context() int64 { return t.Input + t.CacheRead + t.CacheWrite5m + t.CacheWrite1h }

// Cost is a price split by what it paid for.
type Cost struct {
	Input      float64
	Output     float64
	CacheRead  float64
	CacheWrite float64
	WebSearch  float64
}

// Total is the sum of all parts.
func (c Cost) Total() float64 { return c.Input + c.Output + c.CacheRead + c.CacheWrite + c.WebSearch }

// Add returns the sum of two costs.
func (c Cost) Add(o Cost) Cost {
	return Cost{
		Input:      c.Input + o.Input,
		Output:     c.Output + o.Output,
		CacheRead:  c.CacheRead + o.CacheRead,
		CacheWrite: c.CacheWrite + o.CacheWrite,
		WebSearch:  c.WebSearch + o.WebSearch,
	}
}

const perMillion = 1e-6

// Cost prices a token count.
func (r Rates) Cost(t Tokens) Cost {
	return Cost{
		Input:      float64(t.Input) * r.Input * perMillion,
		Output:     float64(t.Output) * r.Output * perMillion,
		CacheRead:  float64(t.CacheRead) * r.CacheRead * perMillion,
		CacheWrite: (float64(t.CacheWrite5m)*r.CacheWrite5m + float64(t.CacheWrite1h)*r.CacheWrite1h) * perMillion,
		WebSearch:  float64(t.WebSearches) * WebSearchDollars,
	}
}

// Entry is one row of the table.
type Entry struct {
	Rates
	// Verified is true when the rates reproduced the cost recorded in real session files.
	Verified bool
}

var builtin = map[string]Entry{
	"claude-sonnet-5-5": {Rates: Rates{Input: 2, Output: 10, CacheRead: 0.10, CacheWrite5m: 2.50, CacheWrite1h: 4}, Verified: true},
	"claude-opus-5-5":   {Rates: Rates{Input: 4, Output: 20, CacheRead: 0.20, CacheWrite5m: 5, CacheWrite1h: 8}, Verified: true},
	// Input, output and web search reproduced a recorded cost. The session files never used the cache on this model, so
	// the three cache rates are a guess that follows the 1.25x and 2x pattern, and the row is not marked verified.
	// Prompts above 100K tokens cost five times as much on this model. The requests Claude Code sends to it are small,
	// so the table does not model that. docs/limits.md says so.
	"claude-haiku-5-5": {Rates: Rates{Input: 0.10, Output: 0.50, CacheRead: 0.01, CacheWrite5m: 0.125, CacheWrite1h: 0.20}},
	// Input, output and cache read are published. The two cache write rates follow the 1.25x and 2x pattern of the other
	// models and have not been checked against a recorded cost.
	"claude-fable-5-2": {Rates: Rates{Input: 8, Output: 40, CacheRead: 0.20, CacheWrite5m: 10, CacheWrite1h: 16}},
}

// Table finds the rates of a model.
type Table struct {
	entries map[string]Entry
}

// NewTable returns the built-in table with overrides added. An override replaces a built-in row of the same name and is
// never marked verified.
func NewTable(overrides map[string]Rates) *Table {
	t := &Table{entries: make(map[string]Entry, len(builtin)+len(overrides))}
	for k, v := range builtin {
		t.entries[k] = v
	}
	for k, v := range overrides {
		t.entries[k] = Entry{Rates: v}
	}
	return t
}

// Lookup returns the rates for a model id. The id may carry a date suffix or a bracketed note such as "[1m]". The
// longest known id that is a prefix of the given one wins, so a dated snapshot finds its family.
func (t *Table) Lookup(model string) (Entry, bool) {
	model = strings.ToLower(strings.TrimSpace(model))
	if i := strings.IndexByte(model, '['); i >= 0 {
		model = model[:i]
	}
	if e, ok := t.entries[model]; ok {
		return e, true
	}
	best := ""
	for k := range t.entries {
		if strings.HasPrefix(model, k+"-") && len(k) > len(best) {
			best = k
		}
	}
	if best == "" {
		return Entry{}, false
	}
	return t.entries[best], true
}

// Models lists the known model ids, sorted.
func (t *Table) Models() []string {
	out := make([]string, 0, len(t.entries))
	for k := range t.entries {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// maxOverrideBytes bounds the file of overrides. A real one is a few hundred bytes.
const maxOverrideBytes = 1 << 20

// ReadOverrides reads a JSON file of the form {"models": {"id": {"input": 3, ...}}}. Unknown fields, negative or
// non-finite numbers and an empty id are errors, so a typo does not silently price a model at zero.
func ReadOverrides(r io.Reader) (map[string]Rates, error) {
	var doc struct {
		Models map[string]Rates `json:"models"`
	}
	dec := json.NewDecoder(io.LimitReader(r, maxOverrideBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("reading rates: %w", err)
	}
	if dec.More() {
		return nil, errors.New("reading rates: more than one JSON value")
	}
	out := make(map[string]Rates, len(doc.Models))
	for id, r := range doc.Models {
		if strings.TrimSpace(id) == "" {
			return nil, errors.New("reading rates: a model id is empty")
		}
		for name, v := range map[string]float64{
			"input": r.Input, "output": r.Output, "cache_read": r.CacheRead,
			"cache_write_5m": r.CacheWrite5m, "cache_write_1h": r.CacheWrite1h,
		} {
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
				return nil, fmt.Errorf("reading rates: %s of %q must be a number of zero or more", name, id)
			}
		}
		out[strings.ToLower(strings.TrimSpace(id))] = r
	}
	return out, nil
}
