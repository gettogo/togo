package omp

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// omp's tool results carry typed parts. Only the text parts reached the
// chat, so a screenshot or a picture a tool read never showed. The part
// shape is omp 18.20's ImageContent ({type, data, mimeType}); no recorded
// image result was available, so the payloads here are built to that shape.
func TestToolResultImagesReachTheTool(t *testing.T) {
	small := base64.StdEncoding.EncodeToString([]byte("\x89PNG small"))
	big := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", harness.MaxInlineImage+1)))
	rec := func(v any) harness.Record {
		b, _ := json.Marshal(v)
		return harness.Record{Source: "session", At: t0, Data: b}
	}
	msg := func(id string, m map[string]any) map[string]any {
		return map[string]any{"type": "message", "id": id, "timestamp": "2026-11-14T00:00:01Z", "message": m}
	}
	m := newSession(env())
	out := run(m, []harness.Record{
		rec(map[string]any{"type": "session", "version": 3, "id": "aaaa1111"}),
		rec(msg("u1", map[string]any{"role": "user", "content": "look"})),
		rec(msg("a1", map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "toolCall", "id": "shot", "name": "browser", "arguments": map[string]any{"action": "screenshot"}},
			map[string]any{"type": "toolCall", "id": "pic", "name": "read", "arguments": map[string]any{"path": "art/logo.png"}},
		}})),
		rec(msg("r1", map[string]any{"role": "toolResult", "toolCallId": "shot", "toolName": "browser", "content": []any{
			map[string]any{"type": "text", "text": "Took a screenshot"},
			map[string]any{"type": "image", "data": small, "mimeType": "image/png"},
			map[string]any{"type": "image", "data": big, "mimeType": "image/jpeg"},
			map[string]any{"type": "image", "data": "!!not base64", "mimeType": "image/png"},
		}})),
	})
	tools := map[string]*harness.ToolBody{}
	for _, it := range out.Items {
		if it.Tool != nil {
			tools[it.ID] = it.Tool
		}
	}
	shot := tools["shot"]
	if shot == nil || len(shot.Images) != 2 {
		t.Fatalf("screenshot tool images = %+v, want the two readable pictures", shot)
	}
	if shot.Images[0].Data == "" || shot.Images[0].MediaType != "image/png" {
		t.Errorf("small picture = %+v, want its data inline", shot.Images[0])
	}
	if shot.Images[1].Data != "" || shot.Images[1].Bytes != harness.MaxInlineImage+1 {
		t.Errorf("large picture = %+v, want type and size only", shot.Images[1])
	}
	if shot.Output != "Took a screenshot" {
		t.Errorf("output = %q, want the text part", shot.Output)
	}
	// A picture named by path is fetched by path, relative to the session folder.
	if pic := tools["pic"]; pic == nil || len(pic.Images) != 1 || !strings.HasSuffix(pic.Images[0].Path, "/art/logo.png") || pic.Images[0].Data != "" {
		t.Errorf("read of a picture = %+v", pic)
	}
}
