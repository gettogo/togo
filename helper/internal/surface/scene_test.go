package surface

import (
	"encoding/json"
	"testing"
)

func sceneMessage(verb byte, body string) Message {
	return Message{Verb: verb, Body: json.RawMessage(body)}
}

func testScene() *Scene {
	return NewScene(RenderOptions{Kinds: []string{"col", "text", "input", "button"}, Cols: 80, Dark: true})
}

func TestSceneNegotiatesOnlyRendererKindsAndDoesNotAckFrames(t *testing.T) {
	s := testScene()
	replies, err := s.Handle(sceneMessage('q', `{"q":"hello","v":[1],"app":"example","features":["send","edit"]}`))
	if err != nil || len(replies) != 1 || replies[0].Verb != 'r' {
		t.Fatalf("hello: %v %v", replies, err)
	}
	var hello struct {
		Kinds []string `json:"kinds"`
	}
	json.Unmarshal(replies[0].Body, &hello)
	if len(hello.Kinds) != 4 || hello.Kinds[3] != "button" {
		t.Fatalf("wrong advertised vocabulary: %v", hello.Kinds)
	}
	s.Handle(sceneMessage('o', `{"id":"one","mode":"inline"}`))
	replies, err = s.Handle(sceneMessage('f', `{"sf":"one","s":1,"ops":[["add","input","one",null,{"id":"input","k":"input","p":{"text":"","sendable":true}}],["focus","input"]]}`))
	if err != nil || len(replies) != 0 {
		t.Fatalf("parser acknowledged a frame: %v %v", replies, err)
	}
}

func TestSceneAdoptsHeldDocumentAndRejectsMissingAdoption(t *testing.T) {
	s := testScene()
	s.Handle(sceneMessage('o', `{"id":"one","mode":"inline"}`))
	s.Handle(sceneMessage('f', `{"sf":"one","s":1,"ops":[["add","text","one",null,{"id":"text","k":"text","p":{"text":"held"}}]]}`))
	s.Handle(sceneMessage('x', `{"id":"one","keep":true}`))
	replies, err := s.Handle(sceneMessage('o', `{"id":"one","mode":"inline","adopt":true}`))
	if err != nil || len(replies) != 0 || s.Views[0].Document.nodes["text"].node.Props["text"] != "held" || s.Views[0].Closed {
		t.Fatalf("held document not adopted: %v %v", replies, err)
	}
	replies, err = s.Handle(sceneMessage('o', `{"id":"missing","mode":"inline","adopt":true}`))
	if err != nil || len(replies) != 1 || replies[0].Verb != 'e' {
		t.Fatalf("missing adoption not reported: %v %v", replies, err)
	}
}

func TestSceneRestoresDocumentIndexesAndRejectsCorruptedSnapshots(t *testing.T) {
	s := testScene()
	s.Handle(sceneMessage('o', `{"id":"one","mode":"inline"}`))
	s.Handle(sceneMessage('f', `{"sf":"one","s":1,"ops":[["add","text","one",null,{"id":"text","k":"text","p":{"text":"held"}}]]}`))
	raw, _ := json.Marshal(s)
	next, err := RestoreScene(raw, s.Options)
	if err != nil {
		t.Fatal(err)
	}
	replies, err := next.Handle(sceneMessage('f', `{"sf":"one","s":2,"ops":[["text","text","append"," after restart"]]}`))
	if err != nil || len(replies) != 0 || next.Views[0].Document.nodes["text"].node.Props["text"] != "held after restart" {
		t.Fatalf("restored document lost its node index: %v %v", replies, err)
	}
	if _, err := RestoreScene([]byte(`{"views":[{"id":"one","document":{"root":{"id":"other","k":"col"}}}]}`), s.Options); err == nil {
		t.Fatal("inconsistent snapshot accepted")
	}
}

func TestFreshHelloAllowsScreenReplacementAndInlineAdoption(t *testing.T) {
	s := testScene()
	s.Handle(sceneMessage('o', `{"id":"screen","mode":"screen"}`))
	s.Handle(sceneMessage('o', `{"id":"inline","mode":"inline"}`))
	s.Handle(sceneMessage('f', `{"sf":"inline","s":1,"ops":[["add","text","inline",null,{"id":"text","k":"text","p":{"text":"held"}}]]}`))
	s.Handle(sceneMessage('q', `{"q":"hello","v":[1],"app":"fixture"}`))
	if _, err := s.Handle(sceneMessage('o', `{"id":"screen","mode":"screen"}`)); err != nil {
		t.Fatal("screen remained live across a new renderer hello", err)
	}
	if _, err := s.Handle(sceneMessage('o', `{"id":"inline","adopt":true}`)); err != nil {
		t.Fatal(err)
	}
	if s.View("inline").Document.nodes["text"].node.Props["text"] != "held" {
		t.Fatal("fresh hello discarded adoptable inline history")
	}
}
