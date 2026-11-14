---
name: leanfox
description: Turns Leanfox lean mode on or off. Only the user types /leanfox. Never use this skill on your own.
---

# leanfox

The user typed `/leanfox`, `/leanfox on`, `/leanfox off` or `/leanfox status`. A hook has already handled it and has added a
message to this conversation that starts with "Leanfox". Follow that message, which says what happened and what to answer, and
do nothing else.

- If there is no such message, the hook did not run, which means the `leanfox` program is not installed or is not on the PATH.
  Tell the user in two lines: run `go install github.com/sungwoonpk/leanfox/cmd/leanfox@latest`, and make sure the folder it
  installs into is on the PATH. Do nothing else.
- Do not turn the mode on or off yourself, and do not call this skill on your own. Only the user's command changes the mode.
