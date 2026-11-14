#!/usr/bin/env python3
"""Breaks the code in small ways, one at a time, and lists the breakages that no test noticed.

Each mutation replaces one piece of text in one file, runs the tests, and puts the file back. A mutation that leaves the tests
green is either a missing test or a change that does not change behavior. docs/validation.md describes the ones found so far.

    python3 scripts/mutation-check.py
"""
import os
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

# (file, text to replace, replacement, what the breakage does)
M = [
 ("internal/pricing/pricing.go", "float64(t.CacheWrite1h)*r.CacheWrite1h", "float64(t.CacheWrite1h)*r.CacheWrite5m", "1h write priced as 5m"),
 ("internal/pricing/pricing.go", 'strings.HasPrefix(model, k+"-")', "strings.HasPrefix(model, k)", "prefix match without dash"),
 ("internal/pricing/pricing.go", "len(k) > len(best)", "len(k) < len(best)", "shortest prefix wins"),
 ("internal/pricing/pricing.go", "t.Input + t.CacheRead + t.CacheWrite5m + t.CacheWrite1h", "t.Input + t.CacheRead + t.CacheWrite5m", "context ignores 1h writes"),
 ("internal/pricing/pricing.go", "|| v < 0 {", "|| v < -1 {", "negative rate allowed"),
 ("internal/transcript/transcript.go", "if t.Output > p.s.Events[i].Tokens.Output {", "if t.Output < p.s.Events[i].Tokens.Output {", "keeps smallest output"),
 ("internal/transcript/transcript.go", "t.CacheWrite5m+t.CacheWrite1h == 0 && u.CacheCreationTokens > 0", "t.CacheWrite5m+t.CacheWrite1h != 0 && u.CacheCreationTokens > 0", "cost fallback inverted"),
 ("internal/transcript/transcript.go", "u.CacheCreation.Ephemeral5m+u.CacheCreation.Ephemeral1h > 0", "u.CacheCreation.Ephemeral5m+u.CacheCreation.Ephemeral1h >= 0", "empty split accepted"),
 ("internal/transcript/transcript.go", "key = \"command\"", "key = \"cmd\"", "wrong bash key"),
 ("internal/transcript/transcript.go", "Chars: utf8.RuneCountInString(text), IsError", "Chars: len(text), IsError", "result size in bytes"),
 ("internal/transcript/transcript.go", "if _, dup := p.blocks[b.ID]; dup {", "if _, dup := p.blocks[b.ID]; !dup {", "duplicate tool call kept"),
 ("internal/analyze/analyze.go", "segmentMinTokens = 20_000", "segmentMinTokens = 1_000", "segment minimum too low"),
 ("internal/analyze/analyze.go", "c*segmentDropRatio < prev", "c*segmentDropRatio > prev", "segment test inverted"),
 ("internal/analyze/analyze.go", "if e.Sidechain {\n				sub++", "if !e.Sidechain {\n				sub++", "sidechain inverted"),
 ("internal/analyze/analyze.go", "const minSameChars = 400", "const minSameChars = 4", "same-output minimum"),
 ("internal/analyze/analyze.go", "delete(readSeen, e.Detail)", "_ = e.Detail", "edit does not reset reads"),
 ("internal/analyze/carry.go", "it.carry = it.tokens * (u.write + float64(it.carried-1)*u.read)", "it.carry = it.tokens * (u.write + float64(it.carried)*u.read)", "carry counts one read too many"),
 ("internal/analyze/carry.go", "if it.at < 1 {\n				continue // already part of the base", "if it.at < 0 {\n				continue // already part of the base", "base items attributed"),
 ("internal/analyze/carry.go", "c.RebuildUSD += float64(extra) * (u.write - u.read)", "c.RebuildUSD += float64(extra) * u.write", "rebuild priced at full write"),
 ("internal/analyze/carry.go", "const minRebuildTokens = 2000", "const minRebuildTokens = 200000", "rebuild threshold huge"),
 ("internal/analyze/carry.go", "grown := max(0, sg.ctx[k]-sg.ctx[k-1])", "grown := sg.ctx[k] - sg.ctx[k-1]", "negative growth allowed"),
 ("internal/analyze/carry.go", "float64(sg.ctx[0])*float64(n-1)*u.read", "float64(sg.ctx[0])*float64(n)*u.read", "base carry off by one"),
 ("internal/analyze/whatif.go", "it.tokens <= float64(limit)", "it.tokens < float64(limit)", "cap boundary"),
 ("internal/analyze/whatif.go", "cut := it.tokens - float64(limit)", "cut := it.tokens", "cap cuts everything"),
 ("internal/analyze/whatif.go", "saved -= float64(base) * (u.write - u.read)", "saved += float64(base) * (u.write - u.read)", "split write loss sign"),
 ("internal/analyze/whatif.go", "if k > 0 && k%every == 0 {", "if k > 0 && k%every == 1 {", "split boundary"),
 ("internal/analyze/whatif.go", "fresh := math.Max(0, float64(base)+float64(ctx[k]-ctx[start]))", "fresh := math.Max(0, float64(base))", "split ignores growth"),
 ("internal/analyze/cost.go", "chk.DiffPercent = (c.Total() - mc.USD) / mc.USD * 100", "chk.DiffPercent = (mc.USD - c.Total()) / mc.USD * 100", "diff sign flipped"),
 ("internal/analyze/cost.go", "r.SubagentUSD += c", "r.SubagentUSD += 0", "sub-agent cost lost"),
 ("internal/analyze/cost.go", "r.Unsplit = math.Max(0, r.TotalUSD-r.Parts.Total())", "r.Unsplit = 0", "unsplit lost"),
 ("internal/analyze/summary.go", "m.Items += sl.Items", "m.Items += 0", "items not summed"),
 ("internal/analyze/summary.go", "if c.Verified && math.Abs(c.DiffPercent) > math.Abs(s.WorstCheckPct) {", "if math.Abs(c.DiffPercent) > math.Abs(s.WorstCheckPct) {", "unverified counted as worst"),
 ("internal/analyze/family.go", '"cd": true, ', "", "cd not a setup statement"),
 ("internal/analyze/family.go", "case w == \"timeout\" && len(words) > 1:\n			words = words[2:]", "case w == \"timeout\" && len(words) > 1:\n			words = words[1:]", "timeout duration kept"),
 ("internal/report/report.go", "case n < 10_000:", "case n < 1_000:", "token format"),
 ("internal/report/report.go", "if math.Abs(unexplained)/inputSide > 0.02 {", "if math.Abs(unexplained)/inputSide > 2 {", "unexplained never shown"),
 ("internal/cli/cli.go", "return 30 * 24 * time.Hour, nil", "return 7 * 24 * time.Hour, nil", "default window"),
 ("internal/cli/cli.go", "if len(as) == 1 {", "if len(as) == 2 {", "single session branch"),
 ("internal/discover/discover.go", "now.Sub(info.ModTime()) > o.Since", "now.Sub(info.ModTime()) < o.Since", "age filter inverted"),
 ("internal/discover/discover.go", "walk(root, 2, add)", "walk(root, 3, add)", "search too deep"),
 ("internal/discover/discover.go", "strings.HasPrefix(filepath.Base(path), o.Session)", "strings.Contains(filepath.Base(path), o.Session)", "session filter contains"),
 ("internal/analyze/carry.go", "written := float64(min(sg.cw[0], sg.ctx[0]))", "written := float64(sg.ctx[0])", "base always written"),
 ("internal/analyze/carry.go", "(float64(sg.ctx[0])-written)*u.read", "(float64(sg.ctx[0])-written)*u.write", "warm base priced as a write"),
 ("internal/hook/command.go", 'if len(fields) > 2 {\n\t\treturn ActionUnknown', 'if len(fields) > 3 {\n\t\treturn ActionUnknown', "extra words accepted"),
 ("internal/hook/command.go", 'case "/leanfox", "/leanfox:leanfox":', 'case "/leanfox":', "namespaced command ignored"),
 ("internal/hook/hook.go", "want := !on", "want := on", "toggle does not toggle"),
 ("internal/hook/hook.go", "case on && st.Told != \"on\":", "case on && st.Told == \"on\":", "rules given when already told"),
 ("internal/hook/hook.go", "if on {\n\t\tparts = append(parts, nudges(", "if !on {\n\t\tparts = append(parts, nudges(", "notices while the mode is off"),
 ("internal/hook/nudge.go", "now.Sub(l.at) > l.ttl", "now.Sub(l.at) < l.ttl", "pause test inverted"),
 ("internal/hook/nudge.go", "st.GapNudged != l.at.Unix()", "st.GapNudged == l.at.Unix()", "pause repeated"),
 ("internal/hook/nudge.go", "l.context >= t && !contains(st.Crossed, t)", "l.context >= t", "size repeated"),
 ("internal/hook/nudge.go", "if e.Tokens.CacheWrite1h > 0 {\n\t\t\tl.ttl = longCache", "if e.Tokens.CacheWrite1h > 0 {\n\t\t\tl.ttl = shortCache", "lifetime ignored"),
 ("internal/hook/nudge.go", "if e.Kind == transcript.KindTurn && !e.Sidechain {", "if e.Kind == transcript.KindTurn {", "sub-agents counted"),
 ("internal/hook/state.go", "id == \"..\"", "id == \"...\"", "dot-dot id accepted"),
 ("internal/hook/state.go", "os.MkdirAll(filepath.Dir(path), 0o700)", "os.MkdirAll(filepath.Dir(path), 0o755)", "state folder not private"),
 ("internal/hook/state.go", "now.Sub(info.ModTime()) > sessionKeepFor", "now.Sub(info.ModTime()) < sessionKeepFor", "pruning inverted"),
 ("internal/hook/hook.go", "for cut > 0 && !utf8.RuneStart(text[cut]) {", "for cut > 0 && false {", "cut inside a character"),
 ("internal/analyze/analyze.go", "if e.Tool == \"Write\" && touched[e.Detail] {", "if e.Tool == \"Write\" && !touched[e.Detail] {", "rewrite inverted"),
 ("internal/transcript/transcript.go", "if size := info.Size(); maxBytes > 0 && size > maxBytes {", "if size := info.Size(); maxBytes > 0 && size < maxBytes {", "tail window inverted"),
]

survived, killed, missing = [], 0, []
for path, old, new, why in M:
    full = os.path.join(ROOT, path)
    with open(full) as f:
        src = f.read()
    if old not in src:
        missing.append((path, why))
        continue
    with open(full, "w") as f:
        f.write(src.replace(old, new, 1))
    try:
        r = subprocess.run(["go", "test", "./internal/...", "-count=1"], cwd=ROOT, capture_output=True, text=True, timeout=300)
        if r.returncode == 0:
            survived.append((path, why))
        else:
            killed += 1
    finally:
        with open(full, "w") as f:
            f.write(src)

print(f"killed {killed}, survived {len(survived)}, pattern not found {len(missing)}")
for s in survived:
    print("SURVIVED:", *s)
for s in missing:
    print("NOT FOUND (the code changed; update the mutation):", *s)
sys.exit(1 if missing else 0)