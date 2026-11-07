package hub

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/session"
	"github.com/gettogo/togo/helper/internal/wire"
)

func TestDecorateAddsMission(t *testing.T) {
	h := New(nil, nil)
	h.SetMission(func(id string) (wire.MissionSummary, bool) {
		if id == "s_bbbbbbbbbb" {
			return wire.MissionSummary{NeedsYou: 1, Plan: &wire.Progress{Done: 1, Total: 3}}, true
		}
		return wire.MissionSummary{}, false
	})
	out := h.Decorate([]session.Session{
		{ID: "s_bbbbbbbbbb", Managed: true},
		{ID: "s_cccccccccc", Managed: true, Mission: &wire.MissionSummary{Agents: 9}},
		{ID: "u_dddddddddd"},
	})
	if out[0].Mission == nil || out[0].Mission.NeedsYou != 1 || out[0].Mission.Plan.Total != 3 {
		t.Fatalf("mission = %+v", out[0].Mission)
	}
	if out[1].Mission != nil || out[2].Mission != nil {
		t.Fatal("a session without an adapter must carry no mission")
	}
}
