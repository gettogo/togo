import SwiftUI
import TogoKit

/// What a roster line says about its session's mission, apart from the view: the model and how
/// full its context is, then the agent and background counts and the plan. "Needs you" is not
/// here; the line's status word carries it.
enum RosterMission {
    /// "opus  ctx 42%  2 agents  1 shell  plan 3/7", parts with nothing to say left out.
    static func parts(meta: SessionMeta, mission: MissionSummary?) -> [String] {
        var parts: [String] = []
        if let model = meta.model?.trimmingCharacters(in: .whitespacesAndNewlines), !model.isEmpty {
            parts.append(model)
        }
        if let percent = meta.contextPercent, percent.isFinite { parts.append(MissionCopy.context(percent)) }
        if let mission {
            var counted = mission
            counted.needsYou = 0
            parts += MissionText.parts(counted)
        }
        return parts
    }

    static func text(meta: SessionMeta, mission: MissionSummary?) -> String {
        parts(meta: meta, mission: mission).joined(separator: "  ")
    }

    /// The same for VoiceOver, with the context and the plan read out.
    static func spoken(meta: SessionMeta, mission: MissionSummary?) -> String {
        var parts: [String] = []
        if let model = meta.model, !model.isEmpty { parts.append(model) }
        if let percent = meta.contextPercent, percent.isFinite { parts.append(MissionCopy.spokenContext(percent)) }
        if let mission {
            var counted = mission
            counted.needsYou = 0
            if !MissionText.parts(counted).isEmpty { parts.append(MissionText.spoken(counted)) }
        }
        return parts.joined(separator: ", ")
    }
}

/// The mission line under a roster line's name: the plan's ring when there is a plan, then the
/// mission words in mono. Nothing when the session has nothing to say.
struct RosterMissionLine: View {
    @ScaledMetric(relativeTo: .caption) private var ring: CGFloat = 10
    let meta: SessionMeta
    let mission: MissionSummary?
    var stale = false

    var body: some View {
        let text = RosterMission.text(meta: meta, mission: mission)
        if !text.isEmpty {
            HStack(alignment: .center, spacing: 6) {
                if let fraction = MissionText.planFraction(mission) {
                    MissionPlanRing(fraction: fraction, side: ring, lineWidth: 1.5)
                        .opacity(stale ? 0.5 : 1)
                }
                Text(verbatim: text)
                    .font(.system(.caption, design: .monospaced))
                    .monospacedDigit()
                    .foregroundStyle(stale ? Color.togoMuteDim : Color.togoMute)
                    .fixedSize(horizontal: false, vertical: true)
                    .contentTransition(.numericText())
            }
            .motion(.fade, value: text)
        }
    }
}

/// Opens a session's mission control from its roster line. Equal by identity of the screen
/// that set it, so setting it again does not redraw every line.
struct OpenSessionMission: Equatable {
    var id = "roster"
    let open: @MainActor (TabID) -> Void

    @MainActor func callAsFunction(_ tab: TabID) { open(tab) }

    static func == (lhs: Self, rhs: Self) -> Bool { lhs.id == rhs.id }
}

extension EnvironmentValues {
    /// nil where the roster cannot open mission control.
    @Entry var openSessionMission: OpenSessionMission? = nil
}

/// "Mission control" in a roster line's long-press menu, where the session has one.
struct RosterMissionMenuButton: View {
    @Environment(\.openSessionMission) private var openMission
    let line: RosterModel.Line

    var body: some View {
        if let openMission, line.mission != nil {
            Button(MissionCopy.title, systemImage: "gauge.with.dots.needle.33percent") { openMission(line.id) }
        }
    }
}

/// How a roster mission sheet hands off when it closes.
enum RosterMissionExit: Equatable {
    /// A diff or a preview for the review.
    case external(HarnessOpen)
    /// The session's own chat.
    case chat
}

/// A session's mission control opened from the roster. Uses the tab's own store when the
/// session is open; otherwise reads the session for as long as the sheet shows.
struct RosterMissionSheet: View {
    let app: AppStore
    let tab: TabID
    let exit: (RosterMissionExit?) -> Void

    @State private var own: HarnessSessionStore?

    var body: some View {
        Group {
            if let store = app.harness(for: tab) ?? own {
                MissionControlStack(
                    store: store, actions: actions,
                    openExternal: { exit(.external($0)) },
                    showChat: { exit(.chat) },
                    onDone: { exit(nil) }
                )
            } else {
                ProgressView(MissionCopy.reading)
                    .foregroundStyle(Color.togoMute)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .presentationDetents([.medium, .large])
        .presentationContentInteraction(.scrolls)
        .presentationDragIndicator(.visible)
        .presentationBackground(Color.togoInk)
        .task { start() }
        .onDisappear {
            own?.stop()
            own = nil
        }
    }

    private func start() {
        guard app.harness(for: tab) == nil, own == nil else { return }
        let capabilities = app.machine(for: tab.hostID)?.capabilities ?? []
        let subject = HarnessSubject(hostID: tab.hostID, sessionID: tab.sessionID, capabilities: capabilities)
        let store = HarnessSessionStore(subject: subject, services: app.harnessServices)
        own = store
        store.launch()
    }

    private var actions: ChatActions {
        let app = self.app
        return ChatActions(
            card: { requestID in app.controlCenter.requests.first { $0.id == requestID } },
            answer: { requestID, decision in Task { await app.answer(requestID, decision) } },
            quote: { _ in }
        )
    }
}

/// A roster tab as a sheet item.
struct RosterMissionTarget: Identifiable, Equatable {
    var tab: TabID
    var id: String { "\(tab.hostID.uuidString)/\(tab.sessionID)" }
}
