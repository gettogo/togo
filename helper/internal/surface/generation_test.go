package surface

import "testing"

func TestWireGenerationChangesWithConversationAndProcessBirth(t *testing.T) {
	r := runtimeInfo{config: CompanionConfig{Generation: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, state: CompanionState{PID: 42, Conversation: "one", Epoch: 1}, birth: "birth-one"}
	first := r.wireGeneration()
	if len(first) != 32 || first != r.wireGeneration() {
		t.Fatal("wire generation is not stable for the same runtime")
	}
	r.state.Conversation = "two"
	if r.wireGeneration() == first {
		t.Fatal("another conversation reused the draft identity")
	}
	r.state.Conversation = "one"
	r.state.Epoch++
	if r.wireGeneration() == first {
		t.Fatal("another conversation epoch reused the draft identity")
	}
	r.state.Epoch--
	r.birth = "birth-two"
	if r.wireGeneration() == first {
		t.Fatal("a replaced process reused the draft identity")
	}
}
