package harness

// Capability is the hello capability a helper lists once it serves the
// harness.* methods and the harness notification.
const Capability = "harness.v1"

// Session capabilities an adapter advertises in harness.describe. The app
// shows a layer, a mission control section or a control only when its flag
// is present; with none, the session shows the terminal only.
const (
	// Conversation.
	CapChat       = "chat"         // items stream for the main conversation
	CapChatStream = "chat.stream"  // assistant text arrives partial, then whole
	CapHistory    = "chat.history" // harness.history pages older items
	CapAnchors    = "chat.anchors" // items carry anchors that harness.locate resolves

	// Composer.
	CapSendAPI   = "send.api"   // prompts go through the harness API, not typing
	CapQueue     = "send.queue" // prompts sent while the agent works wait in the helper
	CapInterrupt = "interrupt"  // harness.interrupt stops the current turn
	CapCommands  = "commands"   // harness.complete kind command lists slash commands
	CapFiles     = "files"      // harness.complete kind file lists @ files
	CapMode      = "mode"       // the mode chip: a mode screen and set_mode action
	CapModel     = "model"      // a model picker screen
	CapEffort    = "effort"     // an effort setting

	// Mission control sections, in display order.
	CapNeedsApprovals = "needs.approvals"
	CapNeedsQuestions = "needs.questions"
	CapAgents         = "agents"
	CapBackground     = "background"
	CapPlan           = "plan"
	CapChanges        = "changes"
	CapPorts          = "ports"
	CapStatus         = "status"
	CapUsage          = "status.usage"
	CapContext        = "status.context"

	// Other screens.
	CapResume   = "resume"
	CapCompact  = "compact"
	CapMCP      = "mcp"
	CapSettings = "settings"
)

// Descriptor is the harness.describe result: which adapter serves a session
// and what it can do.
type Descriptor struct {
	// Adapter is null when no adapter matches; the app shows the terminal.
	Adapter      *AdapterInfo `json:"adapter"`
	Capabilities []string     `json:"capabilities"`
	Screens      []ScreenRef  `json:"screens"`
	// Modes is the mode chip's choices, in the harness's own cycle order.
	Modes []Choice `json:"modes"`
	// Mode is the current mode's id, or empty when unknown.
	Mode string `json:"mode"`
	// Degraded names capabilities the adapter declares but whose sources
	// failed or whose version gate refused this harness version.
	Degraded []string `json:"degraded"`
}

// AdapterInfo names the adapter and the harness version it saw.
type AdapterInfo struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	HarnessVersion string `json:"harnessVersion"`
	// Revision is the adapter file's schema revision.
	Revision int `json:"revision"`
}

// ScreenRef lists a screen in harness.describe without its data.
type ScreenRef struct {
	ID        string `json:"id"`
	Primitive string `json:"primitive"`
	Title     string `json:"title"`
	// Section places the screen: mission control section kind, "composer"
	// (the mode chip and / menu) or "more".
	Section string `json:"section"`
}

// Has reports whether caps holds c.
func Has(caps []string, c string) bool {
	for _, x := range caps {
		if x == c {
			return true
		}
	}
	return false
}
