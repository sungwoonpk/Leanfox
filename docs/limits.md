# Limits and roadmap

## What it promises

- It only reads. It never changes a session file, a setting or anything an agent sees.
- It never prints command arguments or folder names, only the kind of command (`git diff`) and the base name of a file.
- It has no networking code. A test fails if a networking package is linked into the program.
- Where Claude Code recorded a cost, the report shows that cost, and says whether its own price table reproduces it.

## What it cannot do

- **Read anything but Claude Code.** The file format is Claude Code's, it is not documented, and it changes between versions. The
  reader ignores what it does not know, but a change in what it does use (the usage fields, the content blocks, the cost state)
  would make it wrong rather than loud. The `check:` line is what would show it.
- **Know your bill.** The cost is what the usage costs at list prices, which is what Claude Code records. On a subscription plan
  that is not what you are charged.
- **Price every model.** The table has four rows, two of them verified end to end. Any other model is shown as unpriced, and its
  recorded cost is kept in the total as "unsplit". `--rates` adds a model from a JSON file. Prices change, and the table is as old
  as the commit.
- **Read sub-agent files.** Work that Claude Code keeps in separate files under a session folder is not read. The sub-agent work
  that is in the main file is priced, and shown as a part of the total.
- **Tell small groups apart.** The fit measures the density of each kind of text. Kinds that grow together (Claude's replies
  and its reasoning, for example) are hard to separate, so the large groups are reliable and the small ones less so. A very short
  session leans on a weak prior, and its rates are closer to typical values than to measured ones.
- **Follow a conversation across files.** A resumed session that continues in a new file is two sessions to it, and the base of the
  second is priced as it was found.
- **Say what is safe to cut.** See below.

## The what-ifs are not advice

Cutting a tool result, or restarting a conversation from a note, changes what the agent knows. Leanfox can say what each would
save at most. It cannot say what each would cost in quality, because that needs a way to compare the work that the agent does,
and this project has none: it makes no model calls, needs no key and costs nothing to run. Deciding that is the user's job, and
the report is written so that it does not look like it is deciding for them.

## Roadmap

Built:

1. **`leanfox report`.** The measurement described in the other documents.

Candidates, in the order that the measurements suggest, and each only if the measurement pays for it:

2. **Shortening tool output with nothing lost.** Each cut part stays on disk behind a short handle, so the agent can fetch it, and a
   test harness checks that nothing the agent needed was dropped (an error line hidden in noise must survive every filter, no
   text may be invented, and the full output must come back exactly). Before building it, `report` should replay the filters on a
   real session and say what they would have saved, counting the cost of carrying. On the sessions measured, cutting tool results
   has a ceiling of 4% to 11%, which is why it is not first.
3. **A hand-off note for restarting a long conversation.** Built from the session file without a model: what was asked, the files
   changed, the commands that failed, the open to-dos. It would come with a check of what it keeps. This is the larger ceiling
   (36% to 44% at one restart in 100 requests), and also the one that depends most on what the note holds.
4. **A warning before a pause that would rebuild the cache.** Rebuilds were 7% and 17% of the input cost on the long sessions.
5. **Other agents' session formats.**

Nothing after the first has been started.
