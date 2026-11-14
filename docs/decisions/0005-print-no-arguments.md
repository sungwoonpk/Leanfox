# 0005. Nothing private is printed: the kind of a command, the name of a file

Status: accepted

## Context

The report names what was expensive, and an expensive thing is often a command or a file. The arguments of a command are where
tokens, passwords and private hosts end up (`curl -H "Authorization: ..."`), and the path of a file can carry a name or a
customer. Reports are likely to be pasted into issues and chats.

## Decision

A command is named by its kind: the program, and for tools with subcommands the subcommand (`git diff`, `go test`). Leading `cd`,
`export`, assignments and wrappers such as `sudo` are skipped to find it. A file is named by its base name. Nothing else about a
call is stored beyond that, and the JSON output carries the same names.

A test feeds the classifier commands with secrets in them and checks that nothing reaches the output, and another does the same
through a full analysis.

## Costs

- `Bash: python3` says less than the script behind it. That is the point.
- The classifier does not understand quoting, so a quoted `&&` may make it name the wrong part. It names the first usable
  statement and takes only the first words of it, so the worst case is a wrong kind, never leaked text.
- Two files with the same base name are listed under the same name.

## Alternatives considered

- **Print the command, truncated.** Truncation does not remove a secret at the start of a command.
- **Hash the arguments.** Useful for finding repeats, and it is what the tool already does for tool results. It tells a reader
  nothing, so it is not shown.
- **Redact known secret patterns.** A list of patterns is always behind the secrets people actually use.
