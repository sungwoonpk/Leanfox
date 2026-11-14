# 0006. Lean mode is a hook and a few plain instructions, and it never blocks, filters or rewrites

Status: accepted

## Context

After the measurement ([0001](0001-measure-before-reducing.md)), the wish was a switch: type `/leanfox` and the token-saving
version is on, type it again and it is off. Tools of this kind in Claude Code work through a plugin whose hooks add instructions to
the conversation while a flag is set.

The measurements said what such a switch can and cannot reach. Command output is a small share of the cost. A rule can change
habits (an edit instead of a whole-file write), and it cannot make a conversation smaller than the work needs. A pause that let the
cache expire, and a conversation that grew very large, were the big items, and the only thing that can be done about them is to
tell the user in time.

## Decision

- The mode is a plugin: a skill for `/leanfox`, and two hooks (`SessionStart`, `UserPromptSubmit`) that call `leanfox hook`. The
  skill is only there so that the command exists. The hook is what handles it, from the prompt as typed, and also from the
  envelope of tags that other hooks have seen.
- One setting for the machine, and per session a record of what the model was told, so that a session that missed a change in
  another window is told on its next prompt, once.
- The model is told five habits, each of which gives up no information, and the last says that lean never means skipping a check.
  The text is short, because it is itself carried.
- The model is told about a size, a pause and a resume, each once. These only add a sentence. Nothing is blocked or stopped.
- **The hook never blocks a prompt, never filters or shortens a tool's output, and never fails loudly.** It exits 0 for every
  error, validates the session id before using it as a file name, and prints nothing when it cannot do its job.
- The program is Go and is called by name, so the plugin is files only, and does nothing if the program is not installed.

## Costs

- **The rules are instructions, and a model may not follow them.** They reach a few percent of the cost at most on the sessions
  measured, and the effect of the mode has not been measured at all.
- **The notice of a pause comes too late for that pause.** The request that rewrites the cache has already been decided when the
  hook runs. It helps with the next break.
- **It adds about 400 tokens to the conversation**, and a sentence for each notice, all of which are carried too.
- **Two programs have to be installed**: the plugin and the program it calls. A plugin that shipped its own binary would need one
  per platform.
- **The state is machine-wide**, so turning it on in one window turns it on for the next session in every window.
- **Windows is untried**, since the hook command uses `command -v`.

## Alternatives considered

- **Block the prompt after a long pause, and let the user decide.** It would be the only way to act before the rebuild. How a
  blocked prompt is treated differs between descriptions of Claude Code, including whether the prompt stays in the conversation, so
  it was not built on that.
- **Rewrite commands to produce shorter output** (a hook on the Bash tool that changes the command). It is the usual way to
  filter output. It would not reach the largest items, it changes what the user sees run, and how it interacts with permission
  rules that match the command as typed was not established.
- **Replace a tool's output after it ran** (a hook after the tool). Reports from users say this is not honored for the Bash tool in
  some versions, and it would have to be proved lossless before anyone relied on it.
- **Instructions in a CLAUDE.md file.** Always on, no switch, and it would sit in every conversation whether wanted or not.
- **Re-send the rules on every prompt**, as some modes do. The reminder would be carried by every later request, which is the
  cost this project exists to count.
