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
