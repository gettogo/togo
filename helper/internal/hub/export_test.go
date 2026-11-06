package hub

// MarkRelaunched records a relaunch without running one.
func (h *Hub) MarkRelaunched(id string) { h.markRelaunched(id) }
