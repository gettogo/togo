package harness

import "strconv"

// StateMoves is the mapper state key counting the times the mapper
// followed the agent to another conversation while the chat keeps the
// earlier one above it (the agents that report no conversation of their
// own, see the engine's StateConversation). Each time it rises, what
// belonged to the conversation left ends: its running turn and tools,
// open questions, prompts waiting to be sent.
const StateMoves = "conversation_moves"

// Moved raises a mapper's StateMoves.
func Moved(state map[string]string) {
	n, _ := strconv.Atoi(state[StateMoves])
	state[StateMoves] = strconv.Itoa(n + 1)
}
