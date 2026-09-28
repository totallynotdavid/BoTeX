package fsm

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/totallynotdavid/botkit/internal/bot"
)

// The errors Parse wraps. A flow with several mistakes reports all of them,
// each naming its node and transition.
var (
	ErrParse      = errors.New("cannot parse flow")
	ErrStartNode  = errors.New("invalid start node")
	ErrHelpNode   = errors.New("missing help node")
	ErrTarget     = errors.New("transition to a node that does not exist")
	ErrInclude    = errors.New("include of a transition group that does not exist")
	ErrCondition  = errors.New("unknown condition type")
	ErrRegex      = errors.New("regex does not compile")
	ErrValues     = errors.New("invalid condition values")
	ErrMediaKind  = errors.New("unknown media kind")
	errNoValues   = fmt.Errorf("%w: needs at least one", ErrValues)
	errBlankValue = fmt.Errorf("%w: a value is blank", ErrValues)
	errEmptyRegex = fmt.Errorf("%w: needs a regex", ErrValues)
)

//go:embed example.json
var example []byte

// Example returns the flow built into the binary, a small shop that uses every
// feature.
func Example() (*Flow, error) {
	return Parse(example)
}

// Load reads and validates the flow file at path.
func Load(path string) (*Flow, error) {
	data, err := os.ReadFile(path) //nolint:gosec // the operator chooses where the flow lives.
	if err != nil {
		return nil, fmt.Errorf("read flow file: %w", err)
	}

	return Parse(data)
}

// Parse decodes a flow, merges the transition groups nodes include, and
// validates every mistake the router would otherwise meet at runtime.
func Parse(data []byte) (*Flow, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var flow Flow

	err := dec.Decode(&flow)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrParse, err)
	}

	_, err = dec.Token()
	if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: data after the flow", ErrParse)
	}

	err = flow.prepare()
	if err != nil {
		return nil, err
	}

	return &flow, nil
}

// prepare validates the flow, compiling its regexes, then merges the included
// groups into the nodes.
func (f *Flow) prepare() error {
	var errs []error

	if _, ok := f.Nodes[f.StartNode]; !ok {
		errs = append(errs, fmt.Errorf("%w: start_node %q is not a node", ErrStartNode, f.StartNode))
	}

	if _, ok := f.Nodes[HelpNode]; !ok {
		errs = append(errs, fmt.Errorf("%w: no node %q", ErrHelpNode, HelpNode))
	}

	errs = append(errs, f.checkTransitions("global_transitions", f.GlobalTransitions)...)

	for _, name := range slices.Sorted(maps.Keys(f.TransitionGroups)) {
		errs = append(errs, f.checkTransitions(fmt.Sprintf("group %q", name), f.TransitionGroups[name])...)
	}

	for _, id := range slices.Sorted(maps.Keys(f.Nodes)) {
		node := f.Nodes[id]
		errs = append(errs, f.checkTransitions(fmt.Sprintf("node %q transitions", id), node.Transitions)...)

		if _, ok := f.TransitionGroups[node.IncludeTransitions]; node.IncludeTransitions != "" && !ok {
			errs = append(errs, fmt.Errorf("node %q: %w: %q", id, ErrInclude, node.IncludeTransitions))
		}
	}

	err := errors.Join(errs...)
	if err != nil {
		return err
	}

	// Concat copies: nodes that include one group must not share a backing array.
	for id := range f.Nodes {
		node := f.Nodes[id]
		if node.IncludeTransitions != "" {
			node.Transitions = slices.Concat(f.TransitionGroups[node.IncludeTransitions], node.Transitions)
			f.Nodes[id] = node
		}
	}

	return nil
}

// checkTransitions validates each transition of one list, named by where in
// the flow it sits, and compiles its regex in place.
func (f *Flow) checkTransitions(where string, transitions []Transition) []error {
	var errs []error

	for idx := range transitions {
		loc := fmt.Sprintf("%s[%d]", where, idx)

		_, ok := f.Nodes[transitions[idx].Target]
		if !ok {
			errs = append(errs, fmt.Errorf("%s: %w: %q", loc, ErrTarget, transitions[idx].Target))
		}

		err := transitions[idx].Condition.compile()
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", loc, err))
		}
	}

	return errs
}

// compile validates the condition and prepares what matching needs.
func (c *Condition) compile() error {
	switch c.Type {
	case ConditionExact, ConditionKeyword:
		return checkValues(c.Value)
	case ConditionMediaType:
		return checkMediaKinds(c.Value)
	case ConditionRegex:
		return c.compileRegex()
	case ConditionAnyText, ConditionMedia:
		return nil
	}

	return fmt.Errorf("%w: %q", ErrCondition, c.Type)
}

func (c *Condition) compileRegex() error {
	if c.Regex == "" {
		return errEmptyRegex
	}

	// The router lowercases the text, so the pattern must not be case-sensitive.
	re, err := regexp.Compile("(?i)" + c.Regex)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRegex, err)
	}

	c.re = re

	return nil
}

func checkValues(values []string) error {
	if len(values) == 0 {
		return errNoValues
	}

	if slices.ContainsFunc(values, func(value string) bool { return strings.TrimSpace(value) == "" }) {
		return errBlankValue
	}

	return nil
}

func checkMediaKinds(values []string) error {
	err := checkValues(values)
	if err != nil {
		return err
	}

	for _, value := range values {
		switch bot.MediaKind(value) {
		case bot.MediaImage, bot.MediaVideo, bot.MediaAudio, bot.MediaDocument, bot.MediaSticker:
		default:
			return fmt.Errorf("%w: %q", ErrMediaKind, value)
		}
	}

	return nil
}
