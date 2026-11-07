package push

import (
	"encoding/json"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/seal"
	"github.com/gettogo/togo/helper/internal/wire"
)

// Live Activity context limits, in runes. The sealed context rides in the
// content state, which APNs caps at 4 KB with the rest of the payload, so
// the envelope must stay under maxLiveCtx characters of base64url.
const (
	maxLiveTitle   = 64
	maxLiveStep    = 48
	maxLiveAsk     = 160
	maxLiveSubject = 320
	maxLiveCtx     = 2048
	minLiveSubject = 24
)

// Ways the Live Activity can answer a request in place.
const (
	liveAnswerApproveDeny = "approve_deny"
	liveAnswerDeny        = "deny"
)

// LiveContext is what the Live Activity shows beyond its plain fields:
// the session's title, what it is doing, and the request. It is sealed to
// each device like an alert push (label "live", aad "hostId|deviceId"),
// so the relay sees only ciphertext.
type LiveContext struct {
	// Title is the session's name.
	Title string `json:"title,omitempty"`
	// Step is what a working session is doing now, such as the tool it
	// runs.
	Step string `json:"step,omitempty"`
	// Ask is the focused request's question.
	Ask string `json:"ask,omitempty"`
	// Subject is the focused request's command, file, URL or text, already
	// shortened for a glance.
	Subject *wire.Subject `json:"subject,omitempty"`
	// Answer says which buttons can answer the request without the app:
	// approve_deny, deny, or empty for none.
	Answer string `json:"answer,omitempty"`
}

func (c LiveContext) equal(o LiveContext) bool {
	same := c.Subject == nil && o.Subject == nil ||
		c.Subject != nil && o.Subject != nil && *c.Subject == *o.Subject
	return same && c.Title == o.Title && c.Step == o.Step && c.Ask == o.Ask && c.Answer == o.Answer
}

func (c LiveContext) empty() bool { return c.equal(LiveContext{}) }

// requestContext reads what the Live Activity shows about an approval.
func requestContext(a wire.Approval) LiveContext {
	c := LiveContext{Ask: end(detect.CleanLine(a.Title), maxLiveAsk)}
	if a.Subject != nil && a.Subject.Validate() == nil {
		s := shortSubject(*a.Subject, maxLiveSubject)
		c.Subject = &s
	}
	c.Answer = liveAnswer(a)
	return c
}

// liveAnswer is approve_deny when the request has an approve and a deny
// option and can be answered from the phone, deny when it has only deny,
// and empty otherwise: questions and anything else need the app.
func liveAnswer(a wire.Approval) string {
	if a.Answerable != nil && !*a.Answerable {
		return ""
	}
	var approve, deny bool
	for _, o := range a.Options {
		approve = approve || o.Role == wire.RoleApprove
		deny = deny || o.Role == wire.RoleDeny
	}
	switch {
	case approve && deny:
		return liveAnswerApproveDeny
	case deny:
		return liveAnswerDeny
	}
	return ""
}

// sessionTitle is a session's name as a title line.
func sessionTitle(name string) string {
	return end(detect.CleanLine(name), maxLiveTitle)
}

// stepText is a working session's current step.
func stepText(activity *string) string {
	if activity == nil {
		return ""
	}
	return end(detect.CleanLine(*activity), maxLiveStep)
}

// sealLiveContext seals c for one device, shrinking it until the envelope
// fits maxLiveCtx: the step goes first, then the subject is halved down to
// minLiveSubject, then the question is cut, then the title goes. It
// returns "" when c is empty or nothing fits.
func sealLiveContext(pub [32]byte, c LiveContext, aad []byte) string {
	if c.empty() {
		return ""
	}
	for {
		raw, err := json.Marshal(c)
		if err != nil {
			return ""
		}
		ct, err := seal.Seal(pub, raw, seal.LabelLive, aad)
		if err != nil {
			return ""
		}
		switch {
		case len(ct) <= maxLiveCtx:
			return ct
		case c.Step != "":
			c.Step = ""
		case c.Subject != nil && len([]rune(c.Subject.Text)) > minLiveSubject:
			s := shortSubject(*c.Subject, len([]rune(c.Subject.Text))/2)
			c.Subject = &s
		case len([]rune(c.Ask)) > minLiveSubject:
			c.Ask = end(c.Ask, len([]rune(c.Ask))/2)
		case c.Title != "":
			c.Title = ""
		default:
			return ""
		}
	}
}
