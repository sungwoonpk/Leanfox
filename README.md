# Leanfox

[![CI](https://github.com/sungwoonpk/Leanfox/actions/workflows/ci.yml/badge.svg)](https://github.com/sungwoonpk/Leanfox/actions/workflows/ci.yml)
[![CodeQL](https://github.com/sungwoonpk/Leanfox/actions/workflows/codeql.yml/badge.svg)](https://github.com/sungwoonpk/Leanfox/actions/workflows/codeql.yml)

Leanfox reads the session files that Claude Code keeps on your machine and says where your tokens and money went. It measures,
and it does not guess: its price model reproduces the cost that Claude Code recorded to the last stored digit, and it rebuilds
the size of the conversation at each request to within about 1.5%.

It also has a **lean mode** for Claude Code: type `/leanfox` to turn it on, and `/leanfox` again to turn it off. While it is on,
the model is told a few habits that carry fewer tokens without giving up information, and is told when a conversation has grown
large or sat idle past the cache.

Written in Go, with no dependencies. Nothing leaves your machine.

## Why

The usual advice for spending fewer tokens is to make command output shorter. That is the right thing to do if command output is
where the money goes. So Leanfox measures first. On two long sessions of about 800 requests each, the money spent on input went to:

| What the conversation was made of                                     | Session A | Session B |
| --------------------------------------------------------------------- | --------- | --------- |
| What Claude wrote into tool calls (files, scripts, commands)          | 49%       | 48%       |
| What tools returned                                                   | 24%       | 13%       |
| Writing the whole conversation again after a pause outlasted the cache | 7%        | 17%       |
| System prompt, tool definitions, memory                               | 9%        | 8%        |
| Reasoning, replies, my messages, reminders                            | 12%       | 14%       |

Cutting every tool result down to 1,000 tokens would have saved at most 8% and 4%. Starting a fresh conversation every 100
requests from a short note would have saved at most 44% and 36%. Both are ceilings: they say what is worth trying, and they are
not promises. [docs/findings.md](docs/findings.md) has the detail and what it does not show.

The reason is that every request sends the whole conversation again. A token is written to the cache once and then read at
every later request, so what a piece of text costs is its size times the number of requests that came after it.

## Try it

```bash
git clone https://github.com/sungwoonpk/Leanfox.git
cd Leanfox
make demo                  # a made-up session, so you do not need Claude Code history
go run ./cmd/leanfox report                 # your own sessions of the last 30 days
go run ./cmd/leanfox report -json FILE.jsonl
```

Needs Go 1.26 or newer. This is the start of what `make demo` writes:

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

The report goes on with the tool results that cost most to carry, repeated reads, and the two what-if ceilings.

## Lean mode

```sh
go install github.com/sungwoonpk/leanfox/cmd/leanfox@latest      # the program that the hooks call
claude plugin marketplace add sungwoonpk/Leanfox
claude plugin install leanfox@leanfox
```

Then in Claude Code: `/leanfox` (on), `/leanfox` (off), `/leanfox status`. To try it without installing, start Claude Code with
`claude --plugin-dir /path/to/Leanfox`.

What it does is add text to the conversation: five habits (edit a file instead of writing it whole again, do not re-read what has
not changed, ask commands for what is needed but never cut an error, no narration between tool calls, verify as usual), and a
sentence when the conversation first passes a size or sat idle longer than the cache lasts. It never blocks a prompt, and it
never shortens or rewrites what a tool returns.

Whether it saves money has not been measured, and on the sessions above the habits reach a few percent at most. The notices
are about the larger items. [docs/lean-mode.md](docs/lean-mode.md) has the rules, what was checked (in real Claude Code, once),
and how to find out for yourself with `leanfox report`.

## What the report tells you

- **Where the money went**, split by what it paid for, and checked against the cost Claude Code wrote into the file.
- **What the carried conversation was made of**, by kind of text and by tool. The density of each kind is measured from the
  session, not assumed.
- **Cache rebuilds**: pauses long enough for the cache to expire, and what writing the conversation again cost.
- **The single results that cost most to carry**, and **repeats**: a file read again with no edit between, a command that
  gave the same output again.
- **Two what-if ceilings**: cap the size of tool results, and restart from a short note every so many requests.

## What it does not do

The report only reads. Lean mode only adds text, and it does not compress, filter or rewrite what your agent sees. Neither claims
that a saving costs nothing in quality: that depends on whether what was cut was needed, which only the agent's work can show.
The what-ifs are marked as ceilings for that reason, and lean mode's effect is marked as not measured. Command arguments and
folder names are never printed, only the kind of command (`git diff`) and the name of a file. Only Claude Code's session format
is read, and lean mode is untried on Windows.

## How it was checked

- The price table reproduced the recorded cost exactly on every session that had one, for all three models in the table that
  appear in them. [docs/validation.md](docs/validation.md) says what was and was not checked.
- Every session with a recorded cost is explained to within 1.5% by the measured parts and the cache rebuilds.
- 99%+ coverage; the least-squares solver is compared with an exhaustive search on 300 random problems; 59 deliberate bugs were
  put into the code one at a time, and the tests caught 58 (the other one changes nothing that a test could see).
- The reader ignores what it does not know, skips broken lines, and has caps on line length, events and files.
- Lean mode's hook never blocks a prompt, and was run once in real Claude Code: it turned on, turned off, reported its status,
  and the rules were in a new session's context only while it was on ([details](docs/lean-mode.md)).
- CI runs gofmt, vet, staticcheck, errcheck, gosec, race tests, govulncheck and CodeQL, and builds for four targets.

## Docs

| | |
| --- | --- |
| [Lean mode](docs/lean-mode.md) | The rules, the notices, install, what it keeps, what was checked |
| [How it works](docs/how-it-works.md) | The carrying cost, the fit, cache rebuilds, the what-ifs |
| [Findings](docs/findings.md) | What the measurements say, and what they do not |
| [Validation](docs/validation.md) | What was checked against real data, and what was not |
| [Limits and roadmap](docs/limits.md) | What it cannot do, and what comes next |
| [Privacy and safety](docs/privacy-and-safety.md) | What it reads, what it prints, how it handles hostile files |
| [Decisions](docs/decisions/README.md) | Why it is built this way |
| [Development](docs/development.md) | Tests, mutation checking, regenerating the example |

## Status

Two milestones: the `report` command, and lean mode. What comes next is chosen by what the measurements pay for: tool output
that is shortened with every cut part kept and recoverable, and a hand-off note for restarting a long conversation. Neither is
built, and [docs/limits.md](docs/limits.md) says why they wait.
