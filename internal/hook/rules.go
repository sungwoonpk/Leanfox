package hook

// Rules is what the model is told while lean mode is on. It is meant to be short, because it is itself carried by every
// later request, and to cost nothing in quality: each rule is a habit that gives up no information, and the last one says so.
//
// What these rules aim at was measured on real sessions (docs/lean-mode.md): a file written again whole where an edit would
// do, a file read again that had not changed, and command output far larger than what was needed.
const Rules = `LEANFOX MODE IS ON. Same work and same care, less carried: every token stays in the conversation and is read again on every later request.

1. Change an existing file with Edit, not by writing the whole file again. Never paste a file into a Bash heredoc in order to change it.
2. Do not read a file you have already read and not changed. Read a range of a big file, not all of it. Do not run a command again when its output cannot have changed.
3. Ask commands for what you need (grep, head, tail, -q, --stat, a test filter). If a big output may matter, send it to a file and read the part you need. Never cut an error, a failing test or a stack trace: show those in full.
4. No narration between tool calls, no recap of a diff the user can see, no restating the request.
5. Verify as you normally would. Lean never means skipping a test, a check or care. If making something shorter could hide something, do not make it shorter.

The user turns this off with /leanfox.`
