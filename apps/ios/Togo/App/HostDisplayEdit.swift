import Foundation

/// Only fields the user changed in the saved machine's display form.
struct HostDisplayEdit: Sendable {
    var name: String?
    var symbol: String?
    enum Failure: Error { case unavailable }
}

extension AddMachineFlow {
    var validatesConnection: Bool { updateMetadata == nil || displayEdit == nil }

    /// Credentials, routes and connection settings must still match the form as opened.
    var displayEdit: HostDisplayEdit? {
        guard let originalDraft, !draft.trimmedName.isEmpty else { return nil }
        var connection = draft
        connection.name = originalDraft.name
        connection.symbol = originalDraft.symbol
        guard connection == originalDraft else { return nil }
        return HostDisplayEdit(
            name: draft.trimmedName == originalDraft.trimmedName ? nil : draft.trimmedName,
            symbol: draft.symbol == originalDraft.symbol ? nil : draft.symbol.rawValue
        )
    }
}
