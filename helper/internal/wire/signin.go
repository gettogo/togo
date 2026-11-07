package wire

// SignIn is a sign-in step an agent is waiting on, read off its screen: the
// page to open, a device code to enter or confirm there, and whether the
// agent waits for a pasted secret (an API key, or an authorization code
// the page shows). A secret answer is pasted, never typed key by key, and
// is never logged or kept.
type SignIn struct {
	// URL is an http or https page of at most MaxSignInURL bytes; empty
	// when the screen shows none.
	URL string `json:"url,omitempty"`
	// Code is a device code such as ABCD-1234; empty when none is shown.
	Code string `json:"code,omitempty"`
	// Secret means the agent waits for a pasted value; the approval then
	// takes a text answer.
	Secret bool `json:"secret,omitempty"`
}

// MaxSignInURL bounds SignIn.URL, the same bound the browser shims use.
const MaxSignInURL = 4096

// MaxSignInCode bounds SignIn.Code.
const MaxSignInCode = 16
