# Findings

What `leanfox report` said about two long sessions, and what that does and does not show.

## The sessions

Both are the author's own Claude Code work building several small projects, on Claude Sonnet 5.5, of about 800 requests each
(781 and about 850; the second was still running when it was measured). The conversation was 37,000 tokens at the first request, had a
median of about 370,000 and 410,000, and reached about 960,000. The cache hit rate was 99%.

## Where the money went

| | Session A | Session B |
| --- | --- | --- |
| Cache reads (the conversation, read again on every request) | 66% | 55% |
| Output (what the model wrote, reasoning included) | 17% | 23% |
| Cache writes (new text, and rebuilds) | 17% | 22% |
| Input that was not cached | 0% | 0% |

The money for reading the conversation back is the largest part by far. Shortening what the model *writes* addresses the second
part, and only that.

## What the input cost paid for

| | Session A | Session B |
| --- | --- | --- |
| What Claude wrote into tool calls (file contents, scripts, heredocs) | 49% | 48% |
| What the tools returned | 24% | 13% |
| Rebuilding the cache after a pause (3 times, and 4 times) | 7% | 17% |
| System prompt, tool definitions, memory, first message | 9% | 8% |
| Reasoning, replies, the person's messages, reminders | 12% | 14% |

The biggest single thing is not command output. It is the file contents and scripts that the model wrote into `Write`, `Edit`
and `Bash` calls, which stay in the conversation after they have done their job and are read again by every later request. A
tool that only shortens command output cannot reach them.

## What could have been saved (ceilings)

| | Session A | Session B |
| --- | --- | --- |
| Every tool result cut to 500 tokens | 11% | 6% |
| Every tool result cut to 1,000 tokens | 8% | 4% |
| A fresh conversation every 100 requests, from a 3,000-token note | 44% | 36% |
| A fresh conversation every 200 requests | 33% | 24% |

These are the most that could have been saved if nothing else had changed, what was cut was never needed again, and the note
held everything that mattered. The report says so wherever it prints them.

## What this does not show

- **One person, one way of working.** Long sessions that build things and write many files. A session that mostly reads a large
  codebase, or runs a large test suite many times, can look very different. That is the reason the tool exists: run it on yours.
- **Nothing about quality.** The ceilings do not know whether an agent would have done as well from a cut result or from a
  note. They are where to look, not what to expect.
- **No effect of acting on it.** Nobody restarted a conversation or cut a result to see what happened. That is a different
  experiment from this one, and it needs a way to compare the outcomes of the agent's work, which this project does not have.
- **The size of the small groups.** The large groups are reliable to within the fit error. The split between Claude's replies and
  its reasoning, for example, is not.

## One thing the data corrected

The first version treated every record in the file as part of the conversation, and one kind of record (a snapshot of the prompt,
tens of thousands of characters each) made up nearly half of the characters in the "reminders and files" group. It is bookkeeping,
not something the model is sent. The fit is what showed it: the density of that group came out near zero, and a single density
for all text did not fit. Measuring the density of each kind separately fixed it, which is why the rates are measured per kind
([how it works](how-it-works.md)).
