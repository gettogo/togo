package gemini

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// hooks reads Gemini's hook payloads ({session_id, transcript_path, cwd,
// hook_event_name, timestamp, ...}) into state: harness_session,
// transcript_path, working, and chat_glob, the file the chat feed reads (the
// transcript once a hook names it, else the project's chat folder). The
// conversation itself comes from the chat file, so the only items are
// notices for compression and resumption.
type hooks struct {
	env harness.Env

	mu      sync.Mutex
	state   map[string]string
	glob    string
	globAt  time.Time
	notices int
}

func newHooks(env harness.Env) *hooks { return &hooks{env: env, state: map[string]string{}} }

func (h *hooks) Name() string { return MapperHooks }

func (h *hooks) State() map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]string, len(h.state)+1)
	for k, v := range h.state {
		out[k] = v
	}
	if p := h.state["transcript_path"]; p != "" {
		out["chat_glob"] = p
		return out
	}
	// projects.json gains the folder when Gemini starts, so look again
	// while the slug guess has found nothing.
	if h.glob == "" || time.Since(h.globAt) > 5*time.Second {
		h.glob, h.globAt = chatGlob(h.env), time.Now()
	}
	out["chat_glob"] = h.glob
	return out
}

func (h *hooks) Map(rec harness.Record) ([]harness.Item, []harness.Op) {
	var p map[string]any
	if json.Unmarshal(rec.Data, &p) != nil || p == nil {
		return nil, nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if v := str(p, "session_id"); v != "" {
		h.state["harness_session"] = v
	}
	if v := str(p, "transcript_path"); v != "" {
		// The chat feed confines the path when it reads it.
		if filepath.IsAbs(v) && strings.HasSuffix(v, ".json") && !strings.ContainsAny(v, "*?[") {
			h.state["transcript_path"] = v
		}
	}
	at := stamp(str(p, "timestamp"), rec.At)
	switch str(p, "hook_event_name") {
	case "BeforeAgent":
		h.state["working"] = "true"
	case "AfterAgent", "SessionEnd":
		h.state["working"] = "false"
	case "PreCompress":
		return h.notice(at, harness.NoticeCompacted, "Compressing the conversation"), nil
	case "SessionStart":
		h.state["working"] = "false"
		if str(p, "source") == "resume" {
			return h.notice(at, harness.NoticeResumed, "Resumed"), nil
		}
	}
	return nil, nil
}

func (h *hooks) notice(at time.Time, code, text string) []harness.Item {
	h.notices++
	return []harness.Item{{ID: "gemini-notice-" + strconv.Itoa(h.notices) + "-" + at.Format("150405.000"), Kind: harness.KindNotice, At: at,
		Notice: &harness.NoticeBody{Level: "info", Code: code, Text: text}}}
}
