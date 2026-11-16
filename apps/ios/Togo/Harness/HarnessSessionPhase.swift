extension HarnessSessionStore {
    /// Where the conversation stands; cached content stays visible while its feed recovers.
    enum Phase: Equatable {
        /// Asking the helper what serves the session.
        case describing
        /// Opening the conversation.
        case opening
        case live
        /// The connection dropped; trying again.
        case reconnecting
        /// No adapter, no chat, or its source failed: the terminal only.
        case terminalOnly
    }
}
