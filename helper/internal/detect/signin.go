package detect

import (
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/gettogo/togo/helper/internal/wire"
)

// KindSignIn is a sign-in step: a page to open, a device code to enter
// there, or a secret (API key, authorization code) the agent waits for.
const KindSignIn = "signin"

// How far up the screen each part of a sign-in step may sit. A sign-in
// page or code further up has scrolled away: the agent moved on.
const (
	signInURLLines  = 8
	signInCodeLines = 12
	signInHintLines = 6
	// maxSecretPrompt bounds a secret prompt line, so a sentence of agent
	// output that happens to end in a colon does not read as one.
	maxSecretPrompt = 100
)

var (
	webURL = regexp.MustCompile(`https?://[^\s<>"'` + "`" + `]+`)
	// deviceCode is the user code of an OAuth device flow: GitHub, Amp,
	// Kimi, Factory and Charm all show four or five letters and digits,
	// a dash, and four or five more.
	deviceCode = regexp.MustCompile(`\b([A-Z0-9]{4,5}-[A-Z0-9]{4,5})\b`)
	codeWord   = regexp.MustCompile(`(?i)\bcode\b`)
	// secretWord names what a secret prompt asks for.
	secretWord = regexp.MustCompile(`(?i)\b(api[ _-]?key|key|token|secret|password|passphrase)\b`)
	// codeAsk is a prompt for a code the sign-in page shows; "code" alone
	// is too common a word to count.
	codeAsk   = regexp.MustCompile(`(?i)\b(enter|paste|type|input|authori[sz]ation|verification|auth)\b.*\bcode\b|\bcode\b.*\b(here|from)\b`)
	notSecret = regexp.MustCompile(`(?i)\bpress (any|a|the)\b|\bcontinue\b`)
	// secretTail is how a secret prompt line ends: a colon or an arrow,
	// then the empty input or its placeholder.
	secretTail = regexp.MustCompile(`\s*[:>›]\s*[_▪•*█]*\s*$`)
	// inputLine is an empty or masked input under a form label (clack,
	// cliclack): only a cursor or mask characters.
	inputLine = regexp.MustCompile(`^\s*[_▪•*█]*\s*$`)
	// formLabel marks the active field of a clack or cliclack form.
	formLabel = regexp.MustCompile(`^\s*[◆◇▲●]\s+(\S.*)$`)
	// signInHint is how agents point at the page they want opened.
	signInHint = regexp.MustCompile(`(?i)(waiting|navigate to|visit|open (this|the|your)|use this link|did ?n[o']t open|not opening|in your browser|complete (the )?(sign|auth|log)|confirm|authenticat|log ?in|sign ?in)`)
	urlTrail   = ".,;:)]}'\""
)

// matchSignIn recognises a sign-in step at the bottom of the screen: a
// secret prompt on the last line, a device code, or a sign-in page with
// the agent's instructions or "waiting" under it. raw is the window as
// captured (for URLs that wrap), lines the same lines unboxed.
func matchSignIn(raw, lines []string) (Prompt, bool) {
	page := signInURL(raw)
	code := userCode(raw, page)
	if title, ok := secretPrompt(lines); ok {
		return signInPrompt(title, wire.SignIn{URL: page, Code: code, Secret: true}), true
	}
	if code != "" && (page != "" || hinted(raw, signInCodeLines)) {
		return signInPrompt(codeTitle(code, page), wire.SignIn{URL: page, Code: code}), true
	}
	if page != "" && hinted(raw, signInHintLines) {
		return signInPrompt(pageTitle(page), wire.SignIn{URL: page}), true
	}
	return Prompt{}, false
}

func signInPrompt(title string, s wire.SignIn) Prompt {
	return Prompt{Kind: KindSignIn, Title: title, SignIn: &s, Keys: map[int][]string{}}
}

// secretPrompt reads the last line as a prompt for a secret: "Enter the
// authorization code:", "Paste code here if prompted >", or a form field
// labelled "API key" over an empty or masked input.
func secretPrompt(lines []string) (string, bool) {
	// A form's closing corner ("└") sits under its input.
	for len(lines) > 1 && isFiller(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	last := strings.TrimSpace(lines[len(lines)-1])
	if inputLine.MatchString(last) && len(lines) >= 2 {
		if g := formLabel.FindStringSubmatch(lines[len(lines)-2]); g != nil {
			label := strings.TrimSpace(secretTail.ReplaceAllString(g[1], ""))
			if secretLabel(label) {
				return label, true
			}
		}
		return "", false
	}
	if g := formLabel.FindStringSubmatch(last); g != nil {
		last = g[1]
	}
	loc := secretTail.FindStringIndex(last)
	if loc == nil {
		return "", false
	}
	label := strings.TrimSpace(last[:loc[0]])
	if !secretLabel(label) {
		return "", false
	}
	return label, true
}

func secretLabel(label string) bool {
	if label == "" || utf8.RuneCountInString(label) > maxSecretPrompt || len(strings.Fields(label)) > 12 {
		return false
	}
	return (secretWord.MatchString(label) || codeAsk.MatchString(label)) && !notSecret.MatchString(label)
}

// signInURL returns the lowest sign-in page among the bottom lines, with a
// URL that wraps onto the next lines joined back together.
func signInURL(raw []string) string {
	from := max(0, len(raw)-signInURLLines)
	found := ""
	for i := from; i < len(raw); i++ {
		for _, loc := range webURL.FindAllStringIndex(raw[i], -1) {
			u := raw[i][loc[0]:loc[1]]
			if loc[1] == len(raw[i]) {
				u += continuation(raw, i)
			}
			u = strings.TrimRight(u, urlTrail)
			if authURL(u) {
				found = u
			}
		}
	}
	return found
}

// continuation is the rest of a URL that ran to the end of line i and
// wrapped: following lines that are one token of URL characters with
// some URL punctuation, so a plain word under a link is never joined.
func continuation(raw []string, i int) string {
	if utf8.RuneCountInString(raw[i]) < 60 {
		return ""
	}
	var b strings.Builder
	for j := i + 1; j < len(raw) && j <= i+4; j++ {
		t := strings.TrimSpace(raw[j])
		if t == "" || strings.ContainsAny(t, " \t") || !strings.ContainsAny(t, "/?&=%-_.") || webURL.MatchString(t) {
			break
		}
		if strings.ContainsFunc(t, func(r rune) bool { return r > 0x7e || r <= 0x20 }) {
			break
		}
		b.WriteString(t)
		if utf8.RuneCountInString(raw[j]) < 60 {
			break
		}
	}
	return b.String()
}

// authURL reports whether u is a page to sign in on: an OAuth or device
// flow address, by its query or its path.
func authURL(u string) bool {
	if len(u) > wire.MaxSignInURL {
		return false
	}
	p, err := url.Parse(u)
	if err != nil || p.Hostname() == "" {
		return false
	}
	q := p.Query()
	for _, k := range []string{"redirect_uri", "callback_url", "user_code", "code_challenge", "client_id", "response_type"} {
		if q.Has(k) {
			return true
		}
	}
	path := strings.ToLower(p.Path)
	for _, w := range []string{"oauth", "authoriz", "/auth", "login", "signin", "sign-in", "device"} {
		if strings.Contains(path, w) {
			return true
		}
	}
	return false
}

// userCode finds a device code on a line that mentions a code (or just
// under one), or in the page's user_code parameter.
func userCode(raw []string, page string) string {
	from := max(0, len(raw)-signInCodeLines)
	code := ""
	for i := from; i < len(raw); i++ {
		near := codeWord.MatchString(raw[i]) || (i > 0 && codeWord.MatchString(raw[i-1]))
		if !near {
			continue
		}
		// URLs carry dashes and capitals of their own; only text counts.
		text := webURL.ReplaceAllString(raw[i], " ")
		if m := deviceCode.FindStringSubmatch(text); m != nil && strings.ContainsAny(m[1], "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
			code = m[1]
		}
	}
	if code == "" && page != "" {
		if p, err := url.Parse(page); err == nil {
			if c := p.Query().Get("user_code"); deviceCode.MatchString(c) && len(c) <= wire.MaxSignInCode {
				code = c
			}
		}
	}
	return code
}

// hinted reports whether the bottom n lines tell the user to open a page
// or say the agent is waiting on one.
func hinted(raw []string, n int) bool {
	for _, l := range raw[max(0, len(raw)-n):] {
		if signInHint.MatchString(webURL.ReplaceAllString(l, " ")) {
			return true
		}
	}
	return false
}

func codeTitle(code, page string) string {
	host := pageHost(page)
	if host == "" {
		return "Enter code " + code
	}
	if p, err := url.Parse(page); err == nil && p.Query().Get("user_code") == code {
		return "Confirm code " + code + " at " + host
	}
	return "Enter code " + code + " at " + host
}

func pageTitle(page string) string {
	if host := pageHost(page); host != "" && !loopback(host) {
		return "Sign in at " + host
	}
	return "Open the sign-in page"
}

func pageHost(page string) string {
	if page == "" {
		return ""
	}
	p, err := url.Parse(page)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(p.Hostname()), "www.")
}

func loopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || strings.HasSuffix(host, ".localhost")
}
