package wire

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Decision kinds.
const (
	DecisionApprove = "approve"
	DecisionDeny    = "deny"
	DecisionOption  = "option"
	DecisionText    = "text"
)

// MaxDecisionText is the most runes a text decision may carry.
const MaxDecisionText = 2000

// Decision is a parsed approval answer. N is set for option, Text for text.
type Decision struct {
	Kind string
	N    int
	Text string
}

// String returns the wire form ParseDecision accepts.
func (d Decision) String() string {
	switch d.Kind {
	case DecisionOption:
		return DecisionOption + ":" + strconv.Itoa(d.N)
	case DecisionText:
		return DecisionText + ":" + d.Text
	}
	return d.Kind
}

// ParseDecision reads approve, deny, option:<n> (n 1-99, plain decimal) or
// text:<1-2000 runes>. Text decisions reject control characters because the
// text is typed into a terminal, where they would act as keystrokes. Errors
// never quote the input, since answers are not to be logged.
func ParseDecision(s string) (Decision, error) {
	switch s {
	case DecisionApprove, DecisionDeny:
		return Decision{Kind: s}, nil
	}
	if v, ok := strings.CutPrefix(s, DecisionOption+":"); ok {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 99 || strconv.Itoa(n) != v {
			return Decision{}, errors.New("decision option must be 1-99")
		}
		return Decision{Kind: DecisionOption, N: n}, nil
	}
	if v, ok := strings.CutPrefix(s, DecisionText+":"); ok {
		if err := checkText(v); err != nil {
			return Decision{}, err
		}
		return Decision{Kind: DecisionText, Text: v}, nil
	}
	return Decision{}, errors.New("decision must be approve, deny, option:<n> or text:<text>")
}

func checkText(v string) error {
	if v == "" {
		return errors.New("decision text is empty")
	}
	if !utf8.ValidString(v) {
		return errors.New("decision text is not UTF-8")
	}
	if n := utf8.RuneCountInString(v); n > MaxDecisionText {
		return fmt.Errorf("decision text has %d runes, limit %d", n, MaxDecisionText)
	}
	if strings.ContainsFunc(v, unicode.IsControl) {
		return errors.New("decision text contains a control character")
	}
	return nil
}
