package detect

import (
	"regexp"
	"strings"

	"github.com/gettogo/togo/helper/internal/wire"
)

// An MCP server's form as Goose 1.57's CLI asks it, one field at a time
// as a line prompt (goose-cli session/elicitation.rs), seen live:
//
//	Which colour should the banner use?
//	  Options: red, green, blue
//	colour (Banner colour)*:
//
// "*" marks a required field. A field with options takes one of them,
// typed; any other takes the words typed. A boolean field is a clack
// confirm, which matchRadioPair reads.
var (
	fieldLine   = regexp.MustCompile(`^([A-Za-z_][\w.-]*) \(([^()]+)\)(\*?):$`)
	fieldChoice = regexp.MustCompile(`^\s+Options: (\S.*)$`)
)

func matchLineField(lines []string) (Prompt, bool) {
	n := len(lines)
	if n == 0 {
		return Prompt{}, false
	}
	m := fieldLine.FindStringSubmatch(strings.TrimRight(lines[n-1], " "))
	if m == nil {
		return Prompt{}, false
	}
	field := m[1] + " (" + m[2] + ")"
	p := Prompt{Kind: KindQuestion, Title: field, Keys: map[int][]string{}}
	above := n - 2
	if above >= 0 {
		if o := fieldChoice.FindStringSubmatch(lines[above]); o != nil {
			for _, v := range strings.Split(o[1], ", ") {
				if v = strings.TrimSpace(v); v != "" {
					k := len(p.Options) + 1
					p.Options = append(p.Options, wire.Option{N: k, Label: v, Role: wire.RoleOther})
					p.Keys[k] = []string{v, "Enter"}
				}
			}
			above--
		}
	}
	if len(p.Options) == 0 {
		// Words are typed straight into the line.
		p.TextKeys = []string{}
	}
	if above >= 0 {
		if msg := strings.TrimSpace(lines[above]); msg != "" && !fieldLine.MatchString(msg) && !strings.HasPrefix(msg, "▸") && !strings.Contains(msg, ": ") {
			p.Detail = &msg
		}
	}
	return p, true
}
