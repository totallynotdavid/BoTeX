package fsm

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/totallynotdavid/botkit/internal/bot"
)

// Via says which step of the routing produced a Route.
type Via string

const (
	// ViaNode: a transition of the current node matched.
	ViaNode Via = "node"
	// ViaGlobal: a global transition matched.
	ViaGlobal Via = "global"
	// ViaFallback: nothing matched, so the user stays where they are and Action
	// is ActionFallbackWrongMedia or ActionFallbackResponse.
	ViaFallback Via = "fallback"
	// ViaReset: the current node is not in the flow, so the user restarts.
	ViaReset Via = "reset"
)

// Route is where a message takes the user.
type Route struct {
	// Node is the node the user ends up in.
	Node string
	// Action is the action of the transition taken, one of the fallback actions,
	// or empty. The action of the node being entered is not included: the caller
	// runs it, so it also runs when a global transition or a fallback leads there.
	Action string
	Via    Via
}

// DetermineNext routes a message from the current node. media is empty for a
// message without an attachment, and text is then the whole message or, with
// media, its caption.
//
// It tries global transitions before node transitions, so informational and
// safety commands cannot be shadowed by a local fuzzy keyword. A node that
// ignores globals still allows those that lead to HelpNode. Once globals have
// had their chance, node media conditions are tried first when the message has
// an attachment, so a caption does not steal a photo.
func (f *Flow) DetermineNext(current, text string, media bot.MediaKind) Route {
	node, ok := f.Nodes[current]
	if !ok {
		return Route{Node: f.StartNode, Via: ViaReset}
	}

	input := strings.ToLower(strings.TrimSpace(text))

	if hit := f.matchGlobal(input, media, node.IgnoreGlobalTransitions); hit != nil {
		return Route{Node: hit.Target, Action: hit.Action, Via: ViaGlobal}
	}

	if hit := matchNode(node.Transitions, input, media); hit != nil {
		return Route{Node: hit.Target, Action: hit.Action, Via: ViaNode}
	}

	// A file where an image was expected, not just an unmatched message.
	waitsForMedia := slices.ContainsFunc(node.Transitions, func(tr Transition) bool { return tr.Condition.Type == ConditionMediaType })
	if media != "" && waitsForMedia {
		return Route{Node: current, Action: ActionFallbackWrongMedia, Via: ViaFallback}
	}

	return Route{Node: current, Action: ActionFallbackResponse, Via: ViaFallback}
}

// matchNode returns the first of a node's transitions that matches, or nil.
// For a message with media, the media conditions are tried before the others.
func matchNode(transitions []Transition, input string, media bot.MediaKind) *Transition {
	if media != "" {
		for idx := range transitions {
			if cond := &transitions[idx].Condition; cond.isMedia() && cond.matches(input, media) {
				return &transitions[idx]
			}
		}
	}

	for idx := range transitions {
		if transitions[idx].Condition.matches(input, media) {
			return &transitions[idx]
		}
	}

	return nil
}

// matchGlobal returns the first global transition that matches, or nil. A node
// that ignores globals still allows those that lead to HelpNode.
func (f *Flow) matchGlobal(input string, media bot.MediaKind, onlyHelp bool) *Transition {
	for idx := range f.GlobalTransitions {
		tr := &f.GlobalTransitions[idx]
		if (!onlyHelp || tr.Target == HelpNode) && tr.Condition.matches(input, media) {
			return tr
		}
	}

	return nil
}

func (c *Condition) isMedia() bool {
	return c.Type == ConditionMedia || c.Type == ConditionMediaType
}

// matches tests a lowercased, trimmed text and the media kind against the condition.
func (c *Condition) matches(input string, media bot.MediaKind) bool {
	switch c.Type {
	case ConditionExact:
		return slices.ContainsFunc(c.Value, func(value string) bool { return strings.EqualFold(input, value) })
	case ConditionKeyword:
		return matchesKeyword(input, c.Value)
	case ConditionRegex:
		return c.re.MatchString(input)
	case ConditionAnyText:
		return input != "" && media == ""
	case ConditionMedia:
		return media != ""
	case ConditionMediaType:
		return slices.Contains(c.Value, string(media))
	}

	return false
}

// maxExactLetters is the length up to which a word must match a keyword exactly:
// one edit turns "hora" into "hola", so a typo tolerance on short words selects the wrong option.
const maxExactLetters = 4

// maxTypoEdits is the number of edits tolerated in a word longer than maxExactLetters.
const maxTypoEdits = 2

//nolint:gochecknoglobals // a read-only replacer, safe for concurrent use.
var accentFolder = strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u")

// matchesKeyword reports whether the text contains one of the keywords as whole words,
// ignoring case and accents. A single-word keyword also matches a misspelling of it.
func matchesKeyword(text string, keywords []string) bool {
	words := wordsOf(text)

	return slices.ContainsFunc(keywords, func(keyword string) bool {
		phrase := wordsOf(keyword)
		if len(phrase) == 0 {
			// Only symbols, nothing to split on: compare the raw text.
			return strings.Contains(accentFolder.Replace(strings.ToLower(text)), accentFolder.Replace(strings.ToLower(keyword)))
		}

		return containsPhrase(words, phrase)
	})
}

// containsPhrase reports whether the words include the phrase as consecutive words.
// A phrase of one word also matches a misspelling; a longer one must match exactly.
func containsPhrase(words, phrase []string) bool {
	if len(phrase) == 1 {
		return slices.ContainsFunc(words, func(word string) bool { return isSameWord(word, phrase[0]) })
	}

	for start := 0; start+len(phrase) <= len(words); start++ {
		if slices.Equal(words[start:start+len(phrase)], phrase) {
			return true
		}
	}

	return false
}

// wordsOf splits text into lowercase, accent-free words made of letters and digits.
func wordsOf(text string) []string {
	return strings.FieldsFunc(accentFolder.Replace(strings.ToLower(text)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// isSameWord reports whether word is keyword or a typo of it. Lengths count letters, not bytes.
func isSameWord(word, keyword string) bool {
	if min(utf8.RuneCountInString(word), utf8.RuneCountInString(keyword)) <= maxExactLetters {
		return word == keyword
	}

	return levenshtein(word, keyword) <= maxTypoEdits
}
