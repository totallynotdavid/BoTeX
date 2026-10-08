// Package fsm loads a conversation flow and routes a user's messages through
// it: given the node a user is in and what they sent, it says which node comes
// next and which action the transition carries.
package fsm

import "regexp"

// HelpNode is the node a user is sent to when they explicitly ask for a
// person. Global help transitions stay active on nodes that ignore the others.
// Every flow must have it.
const HelpNode = "NEEDS_ASSISTANCE"

// The actions of a Route that stays in the current node because nothing
// matched. They are not transition actions of a flow file.
const (
	// ActionFallbackResponse asks for a reply to a message no transition took.
	ActionFallbackResponse = "trigger_fallback_response"
	// ActionFallbackWrongMedia asks for a reply to a file of a kind the node
	// does not accept, in a node that waits for one.
	ActionFallbackWrongMedia = "trigger_fallback_wrong_media"
)

// ConditionType selects how a Condition tests a message.
type ConditionType string

const (
	// ConditionExact matches when the whole text equals one of the values,
	// ignoring case.
	ConditionExact ConditionType = "exact"
	// ConditionKeyword matches when the text contains one of the values as whole
	// words, ignoring case and accents. A one-word value also matches a
	// misspelling of a word longer than four letters.
	ConditionKeyword ConditionType = "keyword"
	// ConditionRegex matches when the text matches the regex, ignoring case.
	ConditionRegex ConditionType = "regex"
	// ConditionAnyText matches any non-empty text sent without media.
	ConditionAnyText ConditionType = "any_text"
	// ConditionMedia matches any message with media.
	ConditionMedia ConditionType = "media"
	// ConditionMediaType matches media of one of the values, each a bot.MediaKind.
	ConditionMediaType ConditionType = "media_type"
)

// MessageContent is what the bot says on entering a node.
type MessageContent struct {
	Content string `json:"content"`
}

// Condition decides whether a transition applies to a message.
type Condition struct {
	Type  ConditionType `json:"type"`
	Value []string      `json:"value,omitempty"`
	Regex string        `json:"regex,omitempty"`

	re *regexp.Regexp
}

// Transition moves the user to Target when its Condition matches. Action names
// what the runtime does on the way, and may be empty. React is an emoji the bot
// reacts to the message with once the turn has succeeded, and may be empty.
type Transition struct {
	Condition Condition `json:"condition"`
	Target    string    `json:"target"`
	Action    string    `json:"action,omitempty"`
	React     string    `json:"react,omitempty"`
}

// Node is one state of the conversation. Its Action runs when the user enters
// it, whichever transition led there. Its React is the reaction of a turn that
// enters it, when the transition took none.
type Node struct {
	Title                   string         `json:"title,omitempty"`
	Message                 MessageContent `json:"message"`
	Transitions             []Transition   `json:"transitions,omitempty"`
	IncludeTransitions      string         `json:"include_transitions,omitempty"`
	Action                  string         `json:"action,omitempty"`
	React                   string         `json:"react,omitempty"`
	IgnoreGlobalTransitions bool           `json:"ignore_global_transitions,omitempty"`
	FallbackMessage         string         `json:"fallback_message,omitempty"`
}

// Flow is a validated conversation. Build one with Parse, Load or
// EngagingExample. A Flow assembled by hand has no compiled regexes and cannot
// route.
// After Parse, a node's Transitions hold its included group first, then its
// own, and a Flow is safe for concurrent use as long as nobody edits it.
type Flow struct {
	StartNode         string                  `json:"start_node"`
	Nodes             map[string]Node         `json:"nodes"`
	GlobalTransitions []Transition            `json:"global_transitions,omitempty"`
	TransitionGroups  map[string][]Transition `json:"transition_groups,omitempty"`
}
