import Foundation

/// What a link fills in on the new-session sheet. The folder is checked again by the machine
/// when the session starts; here it only has to look like a path.
struct NewSessionPrefill: Equatable, Sendable {
    var hostID: HostID
    var folder: String?
    var presetID: UUID?

    static let maxFolderLength = 1024

    init(hostID: HostID, folder: String?, presetID: UUID?) {
        self.hostID = hostID
        self.folder = folder.flatMap(Self.folder)
        self.presetID = presetID
    }

    /// The folder trimmed, or nil when it is empty, too long or holds control characters.
    static func folder(_ raw: String) -> String? {
        let folder = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !folder.isEmpty, folder.utf8.count <= maxFolderLength,
              !folder.unicodeScalars.contains(where: { $0.properties.generalCategory == .control })
        else { return nil }
        return folder
    }
}

extension NewSessionModel {
    /// Starts the flow from a prefill: the folder step is skipped when a folder came with it,
    /// and the preset is chosen once the machine lists its agents.
    func apply(_ prefill: NewSessionPrefill) {
        guard hostID == prefill.hostID, let machine, !machine.helperMissing else { return }
        requestedPresetID = prefill.presetID
        if let folder = prefill.folder, step == .folder {
            cwd = folder
            step = .agent
        }
    }

    func prefilled(_ prefill: NewSessionPrefill?) -> NewSessionModel {
        if let prefill { apply(prefill) }
        return self
    }
}
