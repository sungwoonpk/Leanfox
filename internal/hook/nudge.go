package hook

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/sungwoonpk/leanfox/internal/pricing"
	"github.com/sungwoonpk/leanfox/internal/transcript"
)

// What is looked at when deciding whether to say something.
const (
	// tailBytes is how much of the end of the session file is read. The latest request is always near the end.
	tailBytes = 4 << 20
	// gapMinContext is the smallest conversation worth mentioning a pause for.
	gapMinContext = 100_000
	// gapMinUSD is the smallest extra cost worth mentioning a pause for.
	gapMinUSD = 0.25
	// shortCache and longCache are the two lifetimes the prompt cache is written with.
	shortCache = 5 * time.Minute
	longCache  = time.Hour
	// recentTurns is how many of the latest requests say which lifetime is in use.
	recentTurns = 5
)

// sizeThresholds are the context sizes, in tokens, that are mentioned once each.
var sizeThresholds = []int64{150_000, 300_000, 500_000, 750_000}

// latest is the newest request of the main conversation, and what it says about the cache.
type latest struct {
	context int64
	model   string
	at      time.Time
	ttl     time.Duration
}

func latestRequest(path string) (latest, bool) {
	if !filepath.IsAbs(path) || !strings.HasSuffix(path, ".jsonl") {
		return latest{}, false
	}
	s, err := transcript.ParseTail(path, tailBytes)
	if err != nil {
		return latest{}, false
	}
	var turns []transcript.Event
	for _, e := range s.Events {
		if e.Kind == transcript.KindTurn && !e.Sidechain {
			turns = append(turns, e)
		}
	}
	if len(turns) == 0 {
		return latest{}, false
	}
	last := turns[len(turns)-1]
	l := latest{context: last.Tokens.Context(), model: last.Model, at: last.Time, ttl: shortCache}
	for _, e := range turns[max(0, len(turns)-recentTurns):] {
		if e.Tokens.CacheWrite1h > 0 {
			l.ttl = longCache
		}
	}
	return l, true
}

// nudges returns what is worth telling the model about the size of the conversation or a pause, and updates what has
// been said. Each thing is said once: a size when it is first passed, a pause once per pause.
func nudges(path string, st *SessionState, now time.Time, tab *pricing.Table) []string {
	l, ok := latestRequest(path)
	if !ok {
		return nil
	}
	rates, priced := tab.Lookup(l.model)
	var out []string

	// A pause that outlasted the cache. The request that follows writes the whole conversation again.
	if !l.at.IsZero() && now.Sub(l.at) > l.ttl && l.context >= gapMinContext && st.GapNudged != l.at.Unix() {
		write := rates.CacheWrite5m
		if l.ttl == longCache {
			write = rates.CacheWrite1h
		}
		extra := float64(l.context) * (write - rates.CacheRead) * 1e-6
		if priced && extra >= gapMinUSD {
			st.GapNudged = l.at.Unix()
			out = append(out, fmt.Sprintf(
				"Leanfox: the conversation sat idle for %s, longer than the cache lasts, so the request that follows writes all of its %s tokens again, about %s more than with a warm cache. "+
					"Tell the user this in one sentence, and that /compact or a fresh session before a long break would avoid paying it for detail they no longer need. Do not stop your work to say it.",
				idle(now.Sub(l.at)), tok(l.context), usd(extra)))
		}
	}

	// A size that has just been passed.
	var passed int64
	for _, t := range sizeThresholds {
		if l.context >= t && !contains(st.Crossed, t) {
			st.Crossed = append(st.Crossed, t)
			passed = t
		}
	}
	if passed > 0 {
		perRequest := ""
		if priced {
			perRequest = fmt.Sprintf(", about %s each at the cache read price", usd(float64(l.context)*rates.CacheRead*1e-6))
		}
		out = append(out, fmt.Sprintf(
			"Leanfox: the conversation is now about %s tokens, and every request reads all of it again%s. "+
				"At a natural stopping point, tell the user in one sentence that /compact or a fresh session would cut that. Do not interrupt work to say it.",
			tok(l.context), perRequest))
	}
	return out
}

func contains(v []int64, x int64) bool {
	for _, y := range v {
		if y == x {
			return true
		}
	}
	return false
}

func tok(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	}
	return fmt.Sprintf("%dk", (n+500)/1000)
}

func usd(v float64) string {
	if v < 0.1 {
		return fmt.Sprintf("$%.3f", v)
	}
	return fmt.Sprintf("$%.2f", v)
}

func idle(d time.Duration) string {
	h, m := int(d.Hours()), int(d.Minutes())%60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}
