package claude

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

func rec(line string) harness.Record {
	return harness.Record{Source: "transcript", At: time.Unix(0, 0), Data: json.RawMessage(line)}
}

func TestPastedImagesBecomeThePromptsImages(t *testing.T) {
	png := base64.StdEncoding.EncodeToString([]byte("tiny png"))
	m := newTranscript(env())
	items, _ := m.Map(rec(`{"type":"user","uuid":"u1","timestamp":"2026-11-07T10:00:00Z","message":{"role":"user","content":[` +
		`{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + png + `"}},` +
		`{"type":"text","text":"[Image #1] what is wrong here?"}]}}`))
	if len(items) != 1 || items[0].User == nil {
		t.Fatalf("items %+v", items)
	}
	u := items[0].User
	if u.Text != "what is wrong here?" || len(u.Images) != 1 || u.Images[0].Data == "" || u.Images[0].MediaType != "image/png" {
		t.Errorf("user %+v", u)
	}
}

func TestReadOfAnImageNamesItsPath(t *testing.T) {
	m := newTranscript(env())
	items, _ := m.Map(rec(`{"type":"assistant","uuid":"a1","timestamp":"2026-11-07T10:00:01Z","message":{"id":"m1","role":"assistant","content":[` +
		`{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"shots/home.png"}}]}}`))
	if len(items) != 1 || items[0].Tool == nil || len(items[0].Tool.Images) != 1 {
		t.Fatalf("items %+v", items)
	}
	if got := items[0].Tool.Images[0].Path; got != "/HOME/proj/shots/home.png" {
		t.Errorf("path %s", got)
	}
}
