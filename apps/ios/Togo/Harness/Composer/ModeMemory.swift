import Foundation
import TogoKit

/// Which mode the state chip shows. The live status line wins, then the last mode the session
/// reported, then what it said when it was described. A mode once known is kept through updates
/// that leave it out. While none is known the chip leaves the mode out; it never shows a word
/// standing in for one.
enum ModeMemory {
    static func current(status: String?, remembered: String?, described: String?) -> String {
        for candidate in [status, remembered, described] {
            if let id = candidate?.trimmingCharacters(in: .whitespaces), !id.isEmpty { return id }
        }
        return ""
    }

    /// The mode a session's mission reports in its status section, if any.
    static func status(_ mission: HarnessMission?) -> String? {
        statusBlock(mission)?.mode
    }

    /// The session's Status block, if its mission has one.
    static func statusBlock(_ mission: HarnessMission?) -> HarnessStatus? {
        mission?.sections.first { $0.kind == .status }?.status
    }
}

extension ModeMemory {
    /// The chip's words for a mode id: the matching choice's label, matched by id or by label
    /// since status lines report either, else the id itself. nil while the mode is unknown.
    static func label(for id: String, in modes: [HarnessChoice]) -> String? {
        guard !id.isEmpty else { return nil }
        let key = id.lowercased()
        let match = modes.first { $0.id.lowercased() == key } ?? modes.first { $0.label.lowercased() == key }
        return match?.label ?? id
    }
}
