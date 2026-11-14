package hook

import "strings"

// Action is what the user asked of lean mode.
type Action int

// The things a prompt can ask.
const (
	// ActionNone means the prompt is not a /leanfox command.
	ActionNone Action = iota
	// ActionToggle turns lean mode on if it is off, and off if it is on.
	ActionToggle
	ActionOn
	ActionOff
	ActionStatus
	// ActionUnknown is /leanfox with an argument that means nothing.
	ActionUnknown
)

// ParseCommand reads a prompt as a /leanfox command. Claude Code is documented as giving a hook the line as it was typed,
// and other hooks have seen it as an envelope of <command-name> and <command-args> tags, so both are read. Anything that
// is not exactly the command, with no more than one argument, is not a command: a sentence that starts with "/leanfox" is
// not mistaken for one, and nothing in the argument is ever echoed.
func ParseCommand(prompt string) Action {
	p := strings.ToLower(strings.Join(strings.Fields(prompt), " "))
	if name, ok := between(p, "<command-name>", "</command-name>"); ok {
		args, _ := between(p, "<command-args>", "</command-args>")
		p = strings.TrimSpace(name + " " + args)
	}
	fields := strings.Fields(p)
	if len(fields) == 0 {
		return ActionNone
	}
	switch fields[0] {
	case "/leanfox", "/leanfox:leanfox":
	default:
		return ActionNone
	}
	if len(fields) == 1 {
		return ActionToggle
	}
	if len(fields) > 2 {
		return ActionUnknown
	}
	switch fields[1] {
	case "on":
		return ActionOn
	case "off":
		return ActionOff
	case "status":
		return ActionStatus
	}
	return ActionUnknown
}

// between returns the text between two markers, trimmed.
func between(s, open, closing string) (string, bool) {
	i := strings.Index(s, open)
	if i < 0 {
		return "", false
	}
	rest := s[i+len(open):]
	j := strings.Index(rest, closing)
	if j < 0 {
		return "", false
	}
	return strings.TrimSpace(rest[:j]), true
}
