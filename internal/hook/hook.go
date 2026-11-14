// Package hook is what runs inside Claude Code's hooks: it turns lean mode on and off when the user types /leanfox, tells
// the model the rules while the mode is on, and says so when a conversation has grown large or sat idle past the cache.
//
// A hook runs on every prompt of every session, and a hook that fails loudly or blocks the prompt is worse than no hook, so
// nothing here ever blocks a prompt, and a hook that cannot do its job prints nothing.
package hook

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sungwoonpk/leanfox/internal/pricing"
)

// The two hook events that are handled.
const (
	EventSessionStart = "session-start"
	EventUserPrompt   = "user-prompt"
)

// Env is what the hooks need from the machine.
type Env struct {
	Store Store
	Now   func() time.Time
	Table *pricing.Table
}

// Input is the part of what Claude Code sends to a hook that is used. The rest is ignored.
type Input struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Prompt         string `json:"prompt"`
	Source         string `json:"source"`
	// Sent on SessionStart when a conversation is resumed.
	CacheLikelyExpired *bool    `json:"prompt_cache_likely_expired"`
	ContextTokens      int64    `json:"context_tokens"`
	EstimatedWriteUSD  *float64 `json:"estimated_cache_write_usd"`
}

type output struct {
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

// maxInput bounds what is read from the hook's input.
const maxInput = 1 << 20

// maxContext is below the 10,000 characters that Claude Code allows a hook to add.
const maxContext = 9000

// Run handles one hook call. It reads the hook's input from in and writes the JSON that adds context to out, or writes
// nothing. The error is for logging: the caller must not turn it into a failure of the user's prompt.
func Run(event string, in io.Reader, out io.Writer, env Env) error {
	var input Input
	if err := json.NewDecoder(io.LimitReader(in, maxInput)).Decode(&input); err != nil {
		return fmt.Errorf("reading hook input: %w", err)
	}
	var parts []string
	var err error
	switch event {
	case EventSessionStart:
		parts, err = sessionStart(&input, env)
	case EventUserPrompt:
		parts, err = userPrompt(&input, env)
	default:
		return fmt.Errorf("unknown hook event %q", event)
	}
	if len(parts) == 0 {
		return err
	}
	text := capContext(strings.Join(parts, "\n\n"))
	var o output
	o.HookSpecificOutput.HookEventName = hookEventName(event)
	o.HookSpecificOutput.AdditionalContext = text
	b, merr := json.Marshal(o)
	if merr != nil {
		return errors.Join(err, merr)
	}
	_, werr := out.Write(append(b, '\n'))
	return errors.Join(err, werr)
}

// capContext keeps what is added under what Claude Code allows, cutting at a character and not in the middle of one.
func capContext(text string) string {
	if len(text) <= maxContext {
		return text
	}
	cut := maxContext
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

func hookEventName(event string) string {
	if event == EventSessionStart {
		return "SessionStart"
	}
	return "UserPromptSubmit"
}

func sessionStart(in *Input, env Env) ([]string, error) {
	st := env.Store.Session(in.SessionID)
	if !env.Store.Mode() {
		st.Told = ""
		return nil, save(env, in.SessionID, st)
	}
	st.Told = "on"
	parts := []string{Rules}
	if in.CacheLikelyExpired != nil && *in.CacheLikelyExpired && in.ContextTokens > 0 {
		cost := ""
		if in.EstimatedWriteUSD != nil && *in.EstimatedWriteUSD > 0 {
			cost = fmt.Sprintf(", about %s to write again", usd(*in.EstimatedWriteUSD))
		}
		parts = append(parts, fmt.Sprintf(
			"Leanfox: this conversation was resumed after its cache had probably expired (about %s tokens%s). "+
				"Tell the user in one sentence, and that /compact or a fresh session would avoid it if the earlier detail is no longer needed.",
			tok(in.ContextTokens), cost))
	}
	return parts, save(env, in.SessionID, st)
}

func userPrompt(in *Input, env Env) ([]string, error) {
	st := env.Store.Session(in.SessionID)
	on := env.Store.Mode()
	var parts []string

	switch act := ParseCommand(in.Prompt); act {
	case ActionToggle, ActionOn, ActionOff:
		want := !on
		if act == ActionOn {
			want = true
		} else if act == ActionOff {
			want = false
		}
		if err := env.Store.SetMode(want); err != nil {
			return []string{"Leanfox could not save the mode, so it is unchanged: " + modeWord(on) + ". Tell the user in one sentence."}, nil
		}
		if want {
			st.Told = "on"
			parts = append(parts, "Leanfox mode was just turned ON by the user. Say so in one short line, then follow these rules for the rest of the conversation.", Rules)
		} else {
			st.Told = "off"
			parts = append(parts, "Leanfox mode was just turned OFF by the user. Say so in one short line, stop following the Leanfox rules from earlier in the conversation, and work as you normally would.")
		}
		return parts, save(env, in.SessionID, st)
	case ActionStatus:
		return []string{"Report this to the user verbatim, in one line, and change nothing: Leanfox mode: " + modeWord(on)}, nil
	case ActionUnknown:
		return []string{"The user typed /leanfox with something that is not recognized, so nothing changed (Leanfox mode: " + modeWord(on) + "). Tell them in one line: /leanfox turns it on or off, and /leanfox on, /leanfox off and /leanfox status are the other forms."}, nil
	}

	// An ordinary prompt. Make the session agree with the mode, which another window may have changed.
	switch {
	case on && st.Told != "on":
		parts = append(parts, Rules)
		st.Told = "on"
	case !on && st.Told == "on":
		parts = append(parts, "Leanfox mode is now OFF. Stop following the Leanfox rules from earlier in the conversation.")
		st.Told = "off"
	}
	if on {
		parts = append(parts, nudges(in.TranscriptPath, &st, env.Now(), env.Table)...)
	}
	return parts, save(env, in.SessionID, st)
}

func modeWord(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// save remembers a session's state. A prompt with no usable session id still works, it just has nothing to remember.
func save(env Env, id string, st SessionState) error {
	if !validID(id) {
		return nil
	}
	return env.Store.SaveSession(id, st, env.Now())
}
