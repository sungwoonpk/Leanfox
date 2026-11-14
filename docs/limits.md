# Limits and roadmap

## What it promises

- The report only reads. It never changes a session file, a setting or anything an agent sees.
- Lean mode only adds text to the conversation. It never blocks a prompt, and never shortens, filters or rewrites what a tool
  returns. Its hook exits 0 whatever goes wrong inside it.
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
- **Make a model follow the rules of lean mode.** They are instructions. The model may not follow them, and what that does to the
  cost was not measured.
- **Undo a rebuild that has already begun.** The notice about a pause is given when the request that rewrites the cache is
  already on its way. It is there so that the next long break can be preceded by a `/compact`.

## The what-ifs are not advice

Cutting a tool result, or restarting a conversation from a note, changes what the agent knows. Leanfox can say what each would
save at most. It cannot say what each would cost in quality, because that needs a way to compare the work that the agent does,
and this project has none: it makes no model calls, needs no key and costs nothing to run. Deciding that is the user's job, and
the report is written so that it does not look like it is deciding for them.

## Roadmap

Built:

1. **`leanfox report`.** The measurement described in the other documents.
2. **Lean mode.** `/leanfox` turns on five habits and the notices about size and pauses ([lean mode](lean-mode.md)). Its effect on
   cost and on quality has not been measured.

Candidates, in the order that the measurements suggest, and each only if the measurement pays for it:

3. **Shortening tool output with nothing lost.** Each cut part stays on disk behind a short handle, so the agent can fetch it, and a
   test harness checks that nothing the agent needed was dropped (an error line hidden in noise must survive every filter, no
   text may be invented, and the full output must come back exactly). Before building it, `report` should replay the filters on a
   real session and say what they would have saved, counting the cost of carrying. On the sessions measured, cutting tool results
   has a ceiling of 4% to 11%, which is why it is not first.
4. **A hand-off note for restarting a long conversation.** Built from the session file without a model: what was asked, the files
   changed, the commands that failed, the open to-dos. It would come with a check of what it keeps. This is the larger ceiling
   (36% to 44% at one restart in 100 requests), and also the one that depends most on what the note holds.
5. **A way to measure what lean mode does.** The same task, with and without it, compared on cost per task and on the work
   produced. It needs a way to compare the work, which is the part this project lacks.
6. **Other agents' session formats.**

Nothing after the second has been started.
