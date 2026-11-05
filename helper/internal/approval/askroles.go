package approval

import (
	"strings"

	"github.com/gettogo/togo/helper/internal/wire"
)

// Whole labels, compared case-insensitively, that read as approving or
// declining. They let the lock screen offer Approve and Deny on an ask.
var (
	approveLabels = []string{"approve", "yes", "allow", "accept", "continue"}
	denyLabels    = []string{"deny", "no", "reject", "cancel", "stop"}
)

// AskRoles gives each ask option a role. approve and deny name the option
// for that role (case-insensitive, whole label) and replace inference for
// it; empty means infer. At most one option takes each role.
func AskRoles(options []string, approve, deny string) []string {
	roles := make([]string, len(options))
	for i := range roles {
		roles[i] = wire.RoleOther
	}
	assign := func(role, explicit string, inferred []string) {
		for i, o := range options {
			label := strings.ToLower(strings.TrimSpace(o))
			match := explicit != "" && label == strings.ToLower(strings.TrimSpace(explicit))
			if explicit == "" {
				for _, w := range inferred {
					match = match || label == w
				}
			}
			if match && roles[i] == wire.RoleOther {
				roles[i] = role
				return
			}
		}
	}
	// Explicit labels go first so inference cannot take their option.
	if approve != "" {
		assign(wire.RoleApprove, approve, nil)
	}
	if deny != "" {
		assign(wire.RoleDeny, deny, nil)
	}
	if approve == "" {
		assign(wire.RoleApprove, "", approveLabels)
	}
	if deny == "" {
		assign(wire.RoleDeny, "", denyLabels)
	}
	return roles
}
