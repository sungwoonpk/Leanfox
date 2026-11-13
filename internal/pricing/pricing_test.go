package pricing

import (
	"math"
	"strings"
	"testing"
)

func near(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s = %.9f, want %.9f", name, got, want)
	}
}

func TestCostOfEachPart(t *testing.T) {
	sonnet, ok := NewTable(nil).Lookup("claude-sonnet-5-5")
	if !ok {
		t.Fatal("sonnet 5.5 is missing from the table")
	}
	// Round numbers, worked out by hand from $2 in, $10 out, $0.10 cache read, $2.50 and $4 cache write.
	c := sonnet.Cost(Tokens{
		Input: 1_000_000, Output: 1_000_000, CacheRead: 10_000_000,
		CacheWrite5m: 1_000_000, CacheWrite1h: 1_000_000, WebSearches: 100,
	})
	near(t, "input", c.Input, 2)
	near(t, "output", c.Output, 10)
	near(t, "cache read", c.CacheRead, 1)
	near(t, "cache write", c.CacheWrite, 6.5)
	near(t, "web search", c.WebSearch, 1)
	near(t, "total", c.Total(), 20.5)
}

func TestOpusAndHaikuRates(t *testing.T) {
	tab := NewTable(nil)
	opus, _ := tab.Lookup("claude-opus-5-5")
	near(t, "opus read", opus.Cost(Tokens{CacheRead: 1_000_000}).CacheRead, 0.20)
	near(t, "opus 1h write", opus.Cost(Tokens{CacheWrite1h: 1_000_000}).CacheWrite, 8)
	haiku, _ := tab.Lookup("claude-haiku-5-5")
	near(t, "haiku in", haiku.Cost(Tokens{Input: 1_000_000}).Input, 0.10)
	near(t, "haiku out", haiku.Cost(Tokens{Output: 1_000_000}).Output, 0.50)
}

func TestVerifiedFlags(t *testing.T) {
	tab := NewTable(nil)
	for id, want := range map[string]bool{
		"claude-sonnet-5-5": true, "claude-opus-5-5": true,
		"claude-haiku-5-5": false, "claude-fable-5-2": false,
	} {
		e, ok := tab.Lookup(id)
		if !ok || e.Verified != want {
			t.Errorf("%s: found %v, verified %v, want verified %v", id, ok, e.Verified, want)
		}
	}
}

func TestLookup(t *testing.T) {
	tab := NewTable(nil)
	for _, id := range []string{
		"claude-sonnet-5-5", "CLAUDE-SONNET-5-5", "  claude-sonnet-5-5 ", "claude-sonnet-5-5[1m]",
		"claude-sonnet-5-5-20261001", "claude-sonnet-5-5-20261001[1m]",
	} {
		if _, ok := tab.Lookup(id); !ok {
			t.Errorf("%q should be found", id)
		}
	}
	for _, id := range []string{"", "gpt-5", "claude-sonnet", "claude-opus-5-55", "claude-sonnet-5-5x", "claude-sonnet-4-6"} {
		if _, ok := tab.Lookup(id); ok {
			t.Errorf("%q should not be found", id)
		}
	}
}

func TestLongestPrefixWins(t *testing.T) {
	tab := NewTable(map[string]Rates{"claude-sonnet-5": {Input: 99}})
	e, ok := tab.Lookup("claude-sonnet-5-5-20261001")
	if !ok || e.Input != 2 {
		t.Fatalf("a dated 5.5 snapshot found %+v, want the 5.5 row and not the shorter family", e)
	}
	e, _ = tab.Lookup("claude-sonnet-5-20260101")
	if e.Input != 99 {
		t.Fatalf("a dated snapshot of the family found %+v, want the override", e)
	}
}

func TestOverridesReplaceAndAreNotVerified(t *testing.T) {
	tab := NewTable(map[string]Rates{"claude-sonnet-5-5": {Input: 7}, "my-model": {Input: 1}})
	e, _ := tab.Lookup("claude-sonnet-5-5")
	if e.Input != 7 || e.Verified {
		t.Errorf("override = %+v, want input 7 and not verified", e)
	}
	if _, ok := tab.Lookup("my-model"); !ok {
		t.Error("a new model from the overrides is missing")
	}
	if got := NewTable(nil).Models(); len(got) != 4 || got[0] != "claude-fable-5-2" {
		t.Errorf("models = %v", got)
	}
}

func TestReadOverrides(t *testing.T) {
	good := `{"models": {" Custom-1 ": {"input": 3, "output": 15, "cache_read": 0.3, "cache_write_5m": 3.75, "cache_write_1h": 6}}}`
	got, err := ReadOverrides(strings.NewReader(good))
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := got["custom-1"]; !ok || r.Output != 15 || r.CacheWrite1h != 6 {
		t.Errorf("got %+v", got)
	}

	bad := map[string]string{
		"a typo in a field":   `{"models": {"m": {"inptu": 3}}}`,
		"an unknown top key":  `{"model": {}}`,
		"a negative rate":     `{"models": {"m": {"input": -1}}}`,
		"an empty id":         `{"models": {" ": {"input": 1}}}`,
		"not JSON":            `input = 3`,
		"two values":          `{"models": {}} {"models": {}}`,
		"a string for a rate": `{"models": {"m": {"input": "3"}}}`,
		"bigger than allowed": `{"models": {"m": {"input": 1}}, "pad": "` + strings.Repeat("x", maxOverrideBytes) + `"}`,
	}
	for name, doc := range bad {
		if _, err := ReadOverrides(strings.NewReader(doc)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestSums(t *testing.T) {
	a := Tokens{Input: 1, Output: 2, CacheRead: 3, CacheWrite5m: 4, CacheWrite1h: 5, WebSearches: 6}
	if got := a.Add(a); got != (Tokens{2, 4, 6, 8, 10, 12}) {
		t.Errorf("Add = %+v", got)
	}
	if got := a.Context(); got != 1+3+4+5 {
		t.Errorf("Context = %d, want input plus every cached part and no output", got)
	}
	c := Cost{1, 2, 3, 4, 5}
	if got := c.Add(c); got != (Cost{2, 4, 6, 8, 10}) || got.Total() != 30 {
		t.Errorf("Cost.Add = %+v", got)
	}
}
