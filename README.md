# Leanfox

[![CI](https://github.com/sungwoonpk/Leanfox/actions/workflows/ci.yml/badge.svg)](https://github.com/sungwoonpk/Leanfox/actions/workflows/ci.yml)
[![CodeQL](https://github.com/sungwoonpk/Leanfox/actions/workflows/codeql.yml/badge.svg)](https://github.com/sungwoonpk/Leanfox/actions/workflows/codeql.yml)

See where your coding agent's tokens and money go. Then turn on a lean mode.

- `leanfox report` reads your Claude Code sessions and splits the cost.
- `/leanfox` turns lean mode on and off inside Claude Code.

Go. No dependencies. Nothing leaves your machine.

## What it found

Two long sessions of about 800 requests. Where the input cost went:

| | Session A | Session B |
| --- | --- | --- |
| What Claude wrote into tool calls | 49% | 48% |
| What tools returned | 24% | 13% |
| Cache rebuilt after a pause | 7% | 17% |
| System prompt, tools, memory | 9% | 8% |
| Everything else | 12% | 14% |

Cutting every tool result to 1,000 tokens saves at most 8% and 4%.
Restarting every 100 requests saves at most 44% and 36%.
Both are ceilings, not promises.

Why: every request re-reads the whole conversation. A token costs its size times the requests after it.

## Try it

```bash
git clone https://github.com/sungwoonpk/Leanfox.git && cd Leanfox
make demo                     # a made-up session, no Claude Code history needed
go run ./cmd/leanfox report   # your own sessions, last 30 days
```

Needs Go 1.26+. The start of the demo:

<!-- sample:start -->
```text
Session demo-ses  (110 requests, 1 segment, started 2026-11-02 09:00, spanning 4h 42m)

COST  $8.34  (recorded by Claude Code)
  cache reads       $2.34   28%   the whole conversation, read again on every request
  output            $2.42   29%   what the model wrote, reasoning included
  cache writes      $3.57   43%   new text entering the conversation, and rebuilds
  input           $0.0004  0.0%   text that was not cached
  check: the price table reproduces the cost Claude Code recorded, exactly

CONTEXT  (what the model read on each request)
  starts at 36k tokens, median 224k, 90th percentile 373k, largest 402k, cache hit rate 96%

WHERE THE INPUT COST CAME FROM  ($5.92; every token is written once, then read again on every later request)
  cache rebuilds   32%  the whole conversation written again, 2 times, after the cache expired in a pause
  tool results     29%  what the tools returned
      Read                             14%   20 results, about 82k tokens
      Bash: go vet                      8%   17 results, about 43k tokens
      Bash: go build                    4%   9 results, about 24k tokens
  tool calls       28%  what Claude wrote into tool calls: files, commands, scripts
      Write                            23%   35 calls, about 151k tokens
      Bash: cat                         5%   12 calls, about 30k tokens
  fixed base        9%  system prompt, tool definitions, memory files, the first message
  claude text     1.0%  Claude's replies
```
<!-- sample:end -->

## Lean mode

```bash
go install github.com/sungwoonpk/leanfox/cmd/leanfox@latest
claude plugin marketplace add sungwoonpk/Leanfox
claude plugin install leanfox@leanfox
```

In Claude Code: `/leanfox` on, `/leanfox` off, `/leanfox status`.

While on, it tells the model five habits and a few notices. It never blocks a prompt or changes tool output.

[More in docs/lean-mode.md](docs/lean-mode.md)

## Honest notes

- The price model matches Claude Code's recorded cost exactly.
- Lean mode's savings are not measured. The habits reach a few percent at most.
- It never prints commands or folder names. Claude Code only.

## Docs

| | |
| --- | --- |
| [Lean mode](docs/lean-mode.md) | Rules, notices, install, what was checked |
| [How it works](docs/how-it-works.md) | Carrying cost, the fit, cache rebuilds |
| [Findings](docs/findings.md) | What the numbers say and do not say |
| [Validation](docs/validation.md) | What was checked against real data |
| [Limits and roadmap](docs/limits.md) | What it cannot do, what is next |
| [Privacy and safety](docs/privacy-and-safety.md) | What it reads and prints |
| [Decisions](docs/decisions/README.md) | Why it is built this way |
| [Development](docs/development.md) | Tests, mutation checks, the example |
