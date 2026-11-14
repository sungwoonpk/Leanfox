# Lean mode

Lean mode is a Claude Code plugin. `/leanfox` turns it on, and `/leanfox` again turns it off. While it is on, the model is told
five habits that carry fewer tokens without giving up any information, and is told when a conversation has grown large or has
sat idle for longer than the prompt cache lasts.

It does not touch what a tool returns, does not block a prompt, and does not make anything shorter behind the model's back.
All it does is add a short text to the conversation, and the model decides what to do with it.

## Install

```sh
go install github.com/sungwoonpk/leanfox/cmd/leanfox@latest      # the program that the hooks call
claude plugin marketplace add sungwoonpk/Leanfox
claude plugin install leanfox@leanfox
```

Make sure the folder that `go install` writes to (`$(go env GOPATH)/bin`) is on your `PATH`. To try it without installing,
start Claude Code with `claude --plugin-dir /path/to/Leanfox`.

It works on macOS and Linux. The hooks use a POSIX shell (`command -v`), and Windows has not been tried.

## Using it

| Type | What happens |
| --- | --- |
| `/leanfox` | Turns lean mode on if it is off, and off if it is on |
| `/leanfox on`, `/leanfox off` | Sets it, whatever it was |
| `/leanfox status` | Says whether it is on, and changes nothing |

The mode is one setting for the whole machine, so it stays on in new sessions until you turn it off. A session that was already
open when you changed it hears about the change on its next prompt. `leanfox mode [on|off|toggle|status]` does the same from a
terminal.

## What the model is told

The rules, about a thousand characters, given when the mode is turned on, at the start of every session while it is on, and again
after a compaction, since the summary may have lost them:

1. Change an existing file with Edit, not by writing the whole file again. Never paste a file into a Bash heredoc in order to
   change it.
2. Do not read a file already read and not changed. Read a range of a big file, not all of it. Do not run a command again when its
   output cannot have changed.
3. Ask commands for what is needed. If a big output may matter, send it to a file and read the part needed. Never cut an error, a
   failing test or a stack trace: show those in full.
4. No narration between tool calls, no recap of a diff the user can see, no restating the request.
5. Verify as normal. Lean never means skipping a test, a check or care. If making something shorter could hide something, do not.

Each rule is a habit, not a cut. None of them removes information that the work needs, and the fifth says so. They are written
down in `internal/hook/rules.go`, and a test keeps them under 2,000 characters, because they are carried by every later request
too: at about 400 tokens, over 800 requests that is a few cents on a session that costs tens of dollars.

### Why these five

They aim at what was measured on two long sessions ([findings](findings.md)):

- **Rule 1.** A `Write` over a file that was already in the conversation carries the whole file again. That happened 19 and 20
  times, and was 7.6% and 5.2% of the input cost. An `Edit` carries only the change. `leanfox report` counts these.
- **Rule 2.** Reading a file again with no edit between was small on those sessions (under half a percent). It is a rule because it
  is free to follow and cannot hurt.
- **Rule 3.** Tool results were 13% to 24% of the input cost, and a few very large ones carried for hundreds of requests.
- **Rule 4.** Text between tool calls is part of what is carried.

The honest reading is that these rules reach a few percent of the cost. The larger things in the measurements (the size a
conversation reaches, and a pause that lets the cache expire) are not things a rule can fix, which is what the notices are for.

## What the model is told as it goes

Only while the mode is on, and each only once:

- **A size.** When the conversation first passes 150,000, 300,000, 500,000 and 750,000 tokens: how large it is, what each request
  costs to read it again (if the model is in the price table), and that at a natural stopping point the user could `/compact` or
  start a fresh session. The model is asked to say this in one sentence and not to interrupt its work.
- **A pause.** When the last response is older than the cache lasts (an hour, or five minutes, depending on which lifetime the
  session writes with), the conversation is at least 100,000 tokens, and writing it again costs at least $0.25 more than reading
  it: how long it sat idle, and what the request that follows will cost to write the conversation again. It cannot undo that
  request. It tells the user, so that the next long break can be preceded by a `/compact`.
- **A resume.** When a session is resumed after its cache probably expired, using the numbers that Claude Code gives the hook.

## What it never does

- **It never blocks a prompt.** A hook that exits with 2 blocks the user's prompt, so the hook command never does. It exits 0 for
  every error and prints nothing when it cannot do its job. `LEANFOX_DEBUG=1` shows what went wrong on stderr.
- **It never shortens, filters or rewrites a tool's output.**
- **It never sends anything anywhere.** The program links no networking package.
- **It never echoes what you typed** after `/leanfox`: an argument that is not understood is reported as not understood.

## What it keeps

A folder of small private files (`$LEANFOX_STATE_DIR`, else `$CLAUDE_CONFIG_DIR/leanfox`, else `~/.claude/leanfox`): a file `mode`,
and for each session a file with what the model was last told, which sizes were mentioned, and when a pause was mentioned.
Session files not touched for 30 days are removed. To read the size and the time of the last request, the hook reads the last
4 MiB of the session file, and keeps none of it.

## Does it help?

That has not been measured, and the project makes no claim about it. The notices are advice that the user may or may not act on,
and the rules depend on the model following them. What can be said is that the rules cost a fraction of a percent, and that
`leanfox report` shows, for any session, the numbers they aim at: the rewrites, the repeats, the share of tool results, and the
cache rebuilds. To find out for yourself, compare a few sessions with the mode on to a few with it off.

Whether quality stays the same is a different question again, and needs a way to compare the work of the agent, which this project
does not have. The rules were written so that they should not change the work (they give up no information), and that is a
design intention and not a result.

## Checked

- Unit tests of the command parser, the state, the rules and every notice, with 96%+ coverage of the hook package.
- The plugin's own hook commands, taken from `hooks/hooks.json`, are run through a shell against the built program, with and
  without it on the `PATH`.
- Once, in real Claude Code 2.1.314 with `--plugin-dir` and a separate state folder: `/leanfox` turned it on and the model said
  so; `/leanfox` again turned it off; `/leanfox status` reported it; a new session with the mode on had the five rules in its
  context, and a new session with it off did not. Six calls to Claude Haiku 5.5, about two cents.

## Uninstall

```sh
claude plugin uninstall leanfox@leanfox
rm -r ~/.claude/leanfox        # or $CLAUDE_CONFIG_DIR/leanfox
```
