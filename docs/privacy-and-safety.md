# Privacy and safety

## What it reads

Session files, and nothing else: the ones named on the command line, or the `*.jsonl` files one folder below
`~/.claude/projects` (or `$CLAUDE_CONFIG_DIR/projects`) that were changed in the last 30 days. A file named with `-rates` is read
too.

From them it keeps sizes, counts, token numbers, model names, times, tool names, and a hash of each tool result. It does not keep
what was said or written, and it does not write anything to disk.

## What it prints

- The **kind** of a shell command (`git diff`, `go test`), never its arguments, because arguments are where secrets and private
  paths are. A test runs a command with a bearer token and a private host in it and checks that neither reaches the report.
- The **base name** of a file (`main.go`), never its folder.
- Numbers, and the model names that appear in the file.

`--json` writes the same things. Both are meant to be safe to paste into an issue.

## Lean mode's hook

The hook runs on every prompt while the plugin is installed, so it is held to a stricter rule than the report: it must not get in
the way.

- It reads what Claude Code sends it (the session id, the path of the session file, the prompt) and, only while the mode is on,
  the last 4 MiB of the session file, to find the size and time of the latest request. It keeps none of that. It keeps the mode and,
  for each session, what the model was last told, which sizes were mentioned and when a pause was mentioned, in private files
  (mode 0600, in a folder with mode 0700). Files for sessions untouched for 30 days are removed.
- It checks a session id before using it in a file name, and refuses anything that could name another place.
- It reads the session file only if the path is absolute and ends in `.jsonl`, and only if it is a regular file.
- It never echoes what the user typed after `/leanfox`.
- It always exits 0 for a known event, because exit code 2 blocks the user's prompt, and it prints nothing when it cannot do its
  job. With `LEANFOX_DEBUG=1` it says on stderr what went wrong.
- What it adds to the conversation is capped at 9,000 characters, under the 10,000 that Claude Code allows, and is cut at a
  character, not in the middle of one.

## Nothing leaves the machine

The program links no networking package, and a test fails if one is added. It makes no model calls and needs no key.

## Hostile and damaged files

A session file is data from a program that changes, and may be damaged or crafted.

- A line that is not JSON, or has the wrong shape, is counted and skipped. The report says how many.
- A line over 64 MiB is skipped without being held in memory.
- Reading stops after 5,000,000 events and the report says so.
- At most 5,000 files are read. Folders are searched one level down, links found inside them are not followed, and only regular
  files are opened, so a folder cannot lead the search somewhere else, and a pipe cannot block it.
- The file of extra prices is limited to 1 MiB, must be a single JSON value, and rejects unknown fields and negative numbers, so a
  typo cannot price a model at zero.
- There is no regular expression built from input.

## What is in the repository

No real session file. `.gitignore` excludes `*.jsonl` except the example, which is made up by a generator in a test, from a fixed
seed. The README sample is checked against it.
