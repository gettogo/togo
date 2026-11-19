package harness

import "strconv"

// StateConversation is the mapper state key naming the conversation the
// agent's terminal shows, for a mapper whose own state belongs to that one
// conversation. When it changes, the engine starts the chat over on the
// new conversation with a fresh mapper, which maps the announcing record
// again. The first conversation named, after a helper restart too, is the
// one the chat shows: naming it again changes nothing.
const StateConversation = "conversation"

// StateMoves is the mapper state key counting the times a mapper followed
// the agent to another conversation when the mapper itself must carry on
// (what it learned picks the conversation: rows seen, lookups, a relaunch's
// session id). Each time it rises the engine starts the chat over on the
// conversation followed, keeping the mapper, which has already reset its
// turn, tools and usage for it. What belonged to the conversation left
// ends: its running turn and tools, open questions, prompts waiting to be
// sent.
const StateMoves = "conversation_moves"

// Moved raises a mapper's StateMoves.
func Moved(state map[string]string) {
	n, _ := strconv.Atoi(state[StateMoves])
	state[StateMoves] = strconv.Itoa(n + 1)
}
