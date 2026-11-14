// Package report writes an analysis for a person to read, or as JSON for another program.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/sungwoonpk/leanfox/internal/analyze"
)

// JSON writes any analysis result as indented JSON.
func JSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Session writes the report of one session.
func Session(w io.Writer, a *analyze.Analysis) {
	si := a.Session
	title := fmt.Sprintf("Session %s", shortID(si.File))
	facts := []string{plural(si.Requests, "request"), plural(si.Segments, "segment")}
	if !si.Started.IsZero() {
		facts = append(facts, "started "+si.Started.Local().Format("2006-01-02 15:04"))
		if d := si.Ended.Sub(si.Started); d > 0 {
			facts = append(facts, "spanning "+duration(d))
		}
	}
	fmt.Fprintf(w, "%s  (%s)\n", title, strings.Join(facts, ", "))
	if si.LinesSkipped > 0 || si.Truncated {
		fmt.Fprintf(w, "  note: %d lines of the file could not be read%s\n", si.LinesSkipped, ifTrue(si.Truncated, " and it was cut short at the event limit", ""))
	}
	fmt.Fprintln(w)

	costSection(w, &a.Cost)
	contextSection(w, &a.Context)
	carrySection(w, a.Carry.Slices, a.Carry.InputSideUSD, a.Carry.Rebuilds, a.Carry.RebuildUSD, a.Carry.Unexplained)
	carriersSection(w, a.Carriers)
	repeatsSection(w, &a.Repeats)
	whatIfSection(w, &a.WhatIf, a.Cost.TotalUSD)
	trustSection(w, a)
}

// Many writes the report of several sessions: a table, then everything added together.
func Many(w io.Writer, as []*analyze.Analysis, s *analyze.Summary) {
	fmt.Fprintf(w, "%s, %s\n\n", plural(s.Sessions, "session"), plural(s.Requests, "request"))
	fmt.Fprintf(w, "  %-9s %-16s %9s %10s %12s %10s\n", "session", "started", "requests", "cost", "largest ctx", "cache hit")
	for _, a := range as {
		started := "-"
		if !a.Session.Started.IsZero() {
			started = a.Session.Started.Local().Format("2006-01-02 15:04")
		}
		fmt.Fprintf(w, "  %-9s %-16s %9d %10s %12s %9.0f%%\n",
			shortID(a.Session.File), started, a.Session.Requests, money(a.Cost.TotalUSD), tokens(a.Context.MaxTokens), a.Context.CacheHitRate*100)
	}
	fmt.Fprintf(w, "  %-9s %-16s %9d %10s\n\n", "all", "", s.Requests, money(s.TotalUSD))

	fmt.Fprintln(w, "ALL OF THEM TOGETHER")
	fmt.Fprintln(w)
	cost := analyze.CostReport{TotalUSD: s.TotalUSD, Parts: s.Parts, Source: analyze.SourceRecorded, Unknown: s.Unknown}
	if s.Recorded < s.Sessions {
		cost.Source = analyze.SourceEstimated
	}
	costParts(w, &cost, s.Recorded, s.Sessions)
	fmt.Fprintln(w)
	carrySection(w, s.Slices, s.InputSideUSD, s.Rebuilds, s.RebuildUSD, s.Unexplained)
	carriersSection(w, s.Carriers)
	repeatsSection(w, &s.Repeats)
	whatIfSection(w, &s.WhatIf, s.TotalUSD)
	fmt.Fprintln(w, "HOW FAR TO TRUST THIS")
	fmt.Fprintf(w, "  The context of each request was rebuilt from its parts to within %s (median, weighted by requests).\n", pct(s.FitError))
	fmt.Fprintf(w, "  Where Claude Code recorded a cost, the price table reproduced it %s (verified models).\n\n", agreement(s.WorstCheckPct))
}

func costSection(w io.Writer, c *analyze.CostReport) {
	costParts(w, c, 0, 0)
	if len(c.Check) > 0 {
		var worst, worstOther float64
		var others []string
		for _, k := range c.Check {
			if k.Verified {
				worst = maxAbs(worst, k.DiffPercent)
			} else {
				worstOther = maxAbs(worstOther, k.DiffPercent)
				others = append(others, k.Model)
			}
		}
		fmt.Fprintf(w, "  check: the price table reproduces the cost Claude Code recorded, %s\n", agreement(worst))
		if math.Abs(worstOther) >= 0.0001 {
			fmt.Fprintf(w, "         unverified rates (%s) differ from the recorded cost by %.2f%%\n", strings.Join(others, ", "), math.Abs(worstOther))
		}
	}
	if len(c.Unknown) > 0 {
		fmt.Fprintf(w, "  not priced, because the model is not in the price table: %s (use --rates to add it)\n", strings.Join(c.Unknown, ", "))
	}
	if c.SubagentUSD > 0 {
		fmt.Fprintf(w, "  sub-agents: %s of the cost above\n", money(c.SubagentUSD))
	}
	fmt.Fprintln(w)
}

func costParts(w io.Writer, c *analyze.CostReport, recorded, sessions int) {
	source := "recorded by Claude Code"
	if c.Source == analyze.SourceEstimated {
		source = "estimated from the tokens of each request"
	}
	if sessions > 0 && recorded < sessions {
		source = fmt.Sprintf("%d of %d sessions recorded by Claude Code, the rest estimated", recorded, sessions)
	}
	fmt.Fprintf(w, "COST  %s  (%s)\n", money(c.TotalUSD), source)
	total := c.Parts.Total()
	if total <= 0 {
		fmt.Fprintln(w)
		return
	}
	row := func(name string, v float64, note string) {
		fmt.Fprintf(w, "  %-13s %9s %5s   %s\n", name, money(v), pct(v/total), note)
	}

	row("cache reads", c.Parts.CacheRead, "the whole conversation, read again on every request")
	row("output", c.Parts.Output, "what the model wrote, reasoning included")
	row("cache writes", c.Parts.CacheWrite, "new text entering the conversation, and rebuilds")
	if c.Parts.Input > 0 {
		row("input", c.Parts.Input, "text that was not cached")
	}
	if c.Parts.WebSearch > 0 {
		row("web search", c.Parts.WebSearch, "server-side searches")
	}
	if c.Unsplit > 0.005 {
		fmt.Fprintf(w, "  %-13s %9s       %s\n", "unsplit", money(c.Unsplit), "cost of models that are not in the price table")
	}
}

func contextSection(w io.Writer, c *analyze.ContextInfo) {
	if c.MaxTokens == 0 {
		return
	}
	fmt.Fprintln(w, "CONTEXT  (what the model read on each request)")
	fmt.Fprintf(w, "  starts at %s tokens, median %s, 90th percentile %s, largest %s, cache hit rate %s\n\n",
		tokens(c.BaseTokens), tokens(c.MedianTokens), tokens(c.P90Tokens), tokens(c.MaxTokens), pct(c.CacheHitRate))
}

var groupNotes = map[string]string{
	analyze.GroupToolCalls:   "what Claude wrote into tool calls: files, commands, scripts",
	analyze.GroupToolResults: "what the tools returned",
	analyze.GroupBase:        "system prompt, tool definitions, memory files, the first message",
	analyze.GroupThinking:    "blocks of reasoning that stay in the conversation",
	analyze.GroupClaudeText:  "Claude's replies",
	analyze.GroupUserText:    "your messages",
	analyze.GroupAttachments: "reminders and files that the program added by itself",
}

const rebuildGroup = "cache rebuilds"

func carrySection(w io.Writer, slices []analyze.Slice, inputSide float64, rebuilds int, rebuildUSD, unexplained float64) {
	if inputSide <= 0 {
		return
	}
	fmt.Fprintf(w, "WHERE THE INPUT COST CAME FROM  (%s; every token is written once, then read again on every later request)\n", money(inputSide))

	type group struct {
		name   string
		usd    float64
		slices []analyze.Slice
	}
	byName := map[string]*group{}
	var groups []*group
	for _, s := range slices {
		g := byName[s.Group]
		if g == nil {
			g = &group{name: s.Group}
			byName[s.Group] = g
			groups = append(groups, g)
		}
		g.usd += s.CarryUSD
		g.slices = append(g.slices, s)
	}
	if rebuilds > 0 {
		groups = append(groups, &group{name: rebuildGroup, usd: rebuildUSD})
	}
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].usd > groups[j].usd })

	for _, g := range groups {
		note := groupNotes[g.name]
		if g.name == rebuildGroup {
			note = fmt.Sprintf("the whole conversation written again, %s, after the cache expired in a pause", plural(rebuilds, "time"))
		}
		fmt.Fprintf(w, "  %-14s %5s  %s\n", g.name, pct(g.usd/inputSide), note)
		sort.SliceStable(g.slices, func(i, j int) bool { return g.slices[i].CarryUSD > g.slices[j].CarryUSD })
		if g.name != analyze.GroupToolCalls && g.name != analyze.GroupToolResults {
			continue // the other groups are one kind of text each
		}
		unit := "call"
		if g.name == analyze.GroupToolResults {
			unit = "result"
		}
		for i, s := range g.slices {
			if i == 3 || s.Share < 0.005 {
				break
			}
			fmt.Fprintf(w, "      %-30s %5s   %s, about %s tokens\n", clip(s.Name, 30), pct(s.Share), plural(s.Items, unit), tokens(s.Tokens))
		}
	}
	if math.Abs(unexplained)/inputSide > 0.02 {
		fmt.Fprintf(w, "  not explained  %5s  the part of the cost that the measured parts do not account for\n", pct(unexplained/inputSide))
	}
	fmt.Fprintln(w)
}

func carriersSection(w io.Writer, cs []analyze.Carrier) {
	if len(cs) == 0 {
		return
	}
	n := min(len(cs), 5)
	fmt.Fprintf(w, "THE %d TOOL RESULTS THAT COST THE MOST TO CARRY\n", n)
	for _, c := range cs[:n] {
		fmt.Fprintf(w, "  %-28s %7s tokens, carried by %5d requests, %8s  %5s\n",
			clip(c.What, 28), tokens(c.Tokens), c.Carried, money(c.CarryUSD), pct(c.Share))
	}
	fmt.Fprintln(w)
}

func repeatsSection(w io.Writer, r *analyze.Repeats) {
	if r.Rereads.Count+r.SameOutputs.Count+r.Rewrites.Count == 0 {
		return
	}
	fmt.Fprintln(w, "REPEATS  (a repeat is sometimes needed: the file may have changed in a way the tool did not show)")
	if r.Rereads.Count > 0 {
		fmt.Fprintf(w, "  %s of a file that was already in the conversation and had not been edited: %s, %s\n",
			plural(r.Rereads.Count, "read"), tokens(r.Rereads.Tokens)+" tokens", money(r.Rereads.USD))
	}
	if r.Rewrites.Count > 0 {
		fmt.Fprintf(w, "  %s written whole over a file that was already in the conversation, where an edit would have carried only the change: %s tokens, %s\n",
			plural(r.Rewrites.Count, "file"), tokens(r.Rewrites.Tokens), money(r.Rewrites.USD))
	}
	if r.SameOutputs.Count > 0 {
		fmt.Fprintf(w, "  %s with the same output as an earlier one (400 characters or more): %s tokens, %s\n",
			plural(r.SameOutputs.Count, "command"), tokens(r.SameOutputs.Tokens), money(r.SameOutputs.USD))
	}
	for _, row := range r.Top {
		fmt.Fprintf(w, "      %-30s %3d times, %s\n", clip(row.What, 30), row.Times, money(row.USD))
	}
	fmt.Fprintln(w)
}

func whatIfSection(w io.Writer, wi *analyze.WhatIf, total float64) {
	if total <= 0 || (len(wi.Cap) == 0 && len(wi.Split) == 0) {
		return
	}
	fmt.Fprintln(w, "WHAT COULD HAVE BEEN SAVED  (ceilings, not promises)")
	fmt.Fprintln(w, "  Cutting tool results down to a size, if what was cut was never needed again:")
	for _, c := range wi.Cap {
		fmt.Fprintf(w, "    cap at %5s tokens   saves %8s %5s   (%s cut)\n", tokens(c.LimitTokens), money(c.SavedUSD), pct(c.SavedUSD/total), plural(c.Affected, "result"))
	}
	fmt.Fprintf(w, "  Starting a fresh conversation every so often from a %s-token note, if the note held everything that mattered:\n", tokens(wi.HandoffTokens))
	for _, c := range wi.Split {
		fmt.Fprintf(w, "    every %4d requests  saves %8s %5s   (%s)\n", c.EveryRequests, money(c.SavedUSD), pct(c.SavedUSD/total), plural(c.Restarts, "restart"))
	}
	fmt.Fprintln(w, "  A negative number means the restart would have cost more than it saved.")
	fmt.Fprintln(w)
}

func trustSection(w io.Writer, a *analyze.Analysis) {
	fmt.Fprintln(w, "HOW FAR TO TRUST THIS")
	fmt.Fprintf(w, "  The context of each request was rebuilt from its parts to within %s (median).\n", pct(a.Carry.FitError))
	if a.Cost.Coverage > 0 {
		fmt.Fprintf(w, "  The file holds the requests behind %s of the cache reads that Claude Code recorded.\n", pct(a.Cost.Coverage))
	}
	fmt.Fprintln(w, "  How much each kind of text weighs is measured, and kinds that grow together are hard to tell apart,")
	fmt.Fprintln(w, "  so the large groups are reliable and the small ones less so.")
	fmt.Fprintln(w)
}

func maxAbs(a, b float64) float64 {
	if math.Abs(b) > math.Abs(a) {
		return b
	}
	return a
}

// agreement says how closely two numbers match, in words a person can check.
func agreement(diffPercent float64) string {
	if math.Abs(diffPercent) < 0.0001 {
		return "exactly"
	}
	return fmt.Sprintf("to within %.4f%%", math.Abs(diffPercent))
}

func money(v float64) string {
	switch {
	case v < 0:
		return "-" + money(-v)
	case v > 0 && v < 0.005:
		return fmt.Sprintf("$%.4f", v)
	}
	return fmt.Sprintf("$%.2f", v)
}

func tokens(n int64) string {
	switch {
	case n < 0:
		return "-" + tokens(-n)
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 10_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	case n < 1_000_000:
		return fmt.Sprintf("%.0fk", float64(n)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1e6)
}

func pct(f float64) string {
	switch {
	case f != 0 && math.Abs(f) < 0.01:
		return fmt.Sprintf("%.1f%%", f*100)
	}
	return fmt.Sprintf("%.0f%%", f*100)
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func duration(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h >= 24 {
		return fmt.Sprintf("%dd %dh", h/24, h%24)
	}
	if h > 0 {
		return fmt.Sprintf("%dh %02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func shortID(file string) string {
	file = strings.TrimSuffix(file, ".jsonl")
	if len(file) > 8 {
		return file[:8]
	}
	return file
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func ifTrue(b bool, yes, no string) string {
	if b {
		return yes
	}
	return no
}
