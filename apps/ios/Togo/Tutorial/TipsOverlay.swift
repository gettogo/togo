import OSLog
import SwiftUI
import TipKit

/// The tips shown the first time a screen appears. Each shows at most once, and is put away for
/// good once the user does what it describes. TipKit can only reset its whole store before it is
/// configured, so "Getting started" instead raises `TutorialModel.tipsGeneration`: every tip gets
/// a new id, which TipKit treats as a tip it has never shown.
enum TutorialTip: String, CaseIterable, Sendable {
    /// The first time a workspace opens: writing to the agent, and its terminal a tap away.
    case workspace
    /// The first request card: approving and denying, here and on the lock screen.
    case request
    /// The first machine shown as Offline.
    case offline

    func tip(generation: Int) -> TogoTip {
        TogoTip(kind: self, generation: generation)
    }

    /// Call when the user does what the tip teaches: opens a workspace, answers a request, or
    /// opens the offline machine.
    func done(defaults: UserDefaults = .standard) {
        tip(generation: defaults.integer(forKey: TutorialModel.Keys.tipsGeneration)).invalidate(reason: .actionPerformed)
    }

    var title: String {
        switch self {
        case .workspace: String(localized: "tutorial.tip.workspace.title", defaultValue: "Chat and terminal")
        case .request: String(localized: "tutorial.tip.request.title", defaultValue: "Answer from here")
        case .offline: String(localized: "tutorial.tip.offline.title", defaultValue: "What Offline means")
        }
    }

    var message: String {
        switch self {
        case .workspace:
            String(
                localized: "tutorial.tip.workspace.message",
                defaultValue: "Write to the agent here. Terminal at the top shows its own screen, with Esc, Ctrl and the arrows above the keyboard."
            )
        case .request:
            String(
                localized: "tutorial.tip.request.message",
                defaultValue: "Approve or deny and the agent carries on. When Togo is closed, the same request reaches your Lock Screen and Apple Watch."
            )
        case .offline:
            String(
                localized: "tutorial.tip.offline.message",
                defaultValue: "Can't reach this machine. It may be asleep. Its sessions come back when it does."
            )
        }
    }

    var systemImage: String {
        switch self {
        case .workspace: "keyboard"
        case .request: "checkmark.bubble"
        case .offline: "moon.zzz"
        }
    }
}

struct TogoTip: Tip {
    let kind: TutorialTip
    let generation: Int

    var id: String { "tutorial.\(kind.rawValue).\(generation)" }
    var title: Text { Text(kind.title) }
    var message: Text? { Text(kind.message) }
    var image: Image? { Image(systemName: kind.systemImage) }
    var options: [any TipOption] { [MaxDisplayCount(1)] }
}

enum TogoTips {
    private static let log = Logger(subsystem: "app.togo.Togo", category: "tips")

    /// Call once at launch, before any view with a tip appears. Without it no tip shows, which
    /// is quiet enough to need a log line.
    static func configure() {
        do {
            try Tips.configure([.displayFrequency(.immediate)])
        } catch {
            log.error("Tips.configure failed: \(error.localizedDescription, privacy: .public)")
        }
    }
}

extension View {
    /// Shows `tip` as a popover the first time this view appears, and again after "Getting started".
    func tutorialTip(_ tip: TutorialTip, arrowEdge: Edge = .top) -> some View {
        modifier(TutorialTipModifier(kind: tip, arrowEdge: arrowEdge))
    }
}

private struct TutorialTipModifier: ViewModifier {
    @AppStorage(TutorialModel.Keys.tipsGeneration) private var generation = 0
    let kind: TutorialTip
    let arrowEdge: Edge

    func body(content: Content) -> some View {
        content
            .popoverTip(kind.tip(generation: generation), arrowEdge: arrowEdge)
            .tint(Color.togoSnow)
    }
}
