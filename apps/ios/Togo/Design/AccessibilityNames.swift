import SwiftUI

/// Names Voice Control matches when the user says "Tap …". A control's spoken label often
/// joins several facts ("04, my-api, Claude Code, Working"), which no one says aloud, so each
/// control also answers to the short words its face shows (HIG accessibility, AX3).
enum VoiceControlNames {
    /// The candidates in order, blanks and repeats (ignoring case) left out. The first is the
    /// name Voice Control shows on its overlay.
    static func make(_ candidates: [String?]) -> [String] {
        var seen = Set<String>()
        var names: [String] = []
        for candidate in candidates {
            guard let name = candidate?.trimmingCharacters(in: .whitespacesAndNewlines), !name.isEmpty else { continue }
            guard seen.insert(name.lowercased()).inserted else { continue }
            names.append(name)
        }
        return names
    }

    /// A session as its line or chip shows it: its name, its bib with the name, the bib alone
    /// and its agent.
    static func session(name: String, bib: String?, agent: String? = nil) -> [String] {
        make([name, bib.map { "\($0) \(name)" }, bib, agent])
    }
}

extension View {
    /// Voice Control names; a no-op when there are none, so the label stays the default name.
    @ViewBuilder func voiceControlNames(_ names: [String]) -> some View {
        if names.isEmpty {
            self
        } else {
            accessibilityInputLabels(names)
        }
    }
}
