import SnapshotTesting
import SwiftUI
import Testing
import TogoPush
@testable import Togo

private let now = Date(timeIntervalSince1970: 1_793_707_200)
/// Stands in for a wallpaper so the card and island edges show in the reference.
private let wallpaper = Color(red: 0.42, green: 0.45, blue: 0.50)

private typealias State = TogoActivityAttributes.ContentState

private func state(
    _ status: String, waiting: Int = 1, command: String? = nil, ago: TimeInterval = 252, bib: String? = "04"
) -> State {
    State(
        bib: bib, agent: "Claude Code", status: status, waiting: waiting,
        since: Int(now.timeIntervalSince1970 - ago), requestId: status == "needs_you" ? "r_1" : nil, command: command
    )
}

/// The context the helper seals for the request and session these states show.
private func card(_ state: State, isStale: Bool = false) -> LiveCard {
    let context = LiveContext(
        title: "Fix the login redirect", step: state.status == "working" ? "Bash" : nil,
        ask: state.status == "needs_you" ? "Run this command?" : nil,
        subject: state.command.map { PushSubject(kind: "command", text: $0) }, answer: .approveDeny
    )
    return LiveCard(
        agent: "claude", status: state.status, waiting: state.waiting, requestId: state.requestId, command: nil,
        context: context, machine: "Studio", stale: isStale
    )
}

private let cases: [(name: String, state: State)] = [
    ("needs-you", state("needs_you")),
    ("needs-you-command", state(
        "needs_you",
        command: "rm -rf ./build && xcodebuild -scheme Togo -destination 'platform=iOS Simulator,name=iPhone 17' test"
    )),
    ("working", state("working", waiting: 0, ago: 1_830)),
    ("done", state("done", waiting: 0, ago: 40)),
    ("ended", state("ended", waiting: 0, ago: 40)),
    ("failed", state("failed", waiting: 0, ago: 40)),
    ("offline", state("offline", waiting: 2, ago: 400)),
    ("three-waiting", state("needs_you", waiting: 3)),
]

/// The lock screen card as the system draws it: the view on its tint, inset from the screen
/// edge, with the system's corner radius.
private struct LockScreenFrame: View {
    let state: State
    var isStale = false

    var body: some View {
        LiveLockScreenView(state: state, card: card(state, isStale: isStale), isStale: isStale)
            .background(Color.togoInk)
            .clipShape(RoundedRectangle(cornerRadius: 24, style: .continuous))
            .padding(.horizontal, 16)
            .frame(maxHeight: .infinity)
            .background(wallpaper)
            .environment(\.liveClockFrozenAt, now)
    }
}

/// The island's three presentations around a camera cutout, the way the system arranges the
/// regions.
private struct IslandGallery: View {
    let state: State
    var isStale = false

    var body: some View {
        VStack(spacing: 28) {
            HStack(spacing: 0) {
                IslandCompactLeading(card: card(state, isStale: isStale)).padding(.leading, 18)
                Spacer(minLength: 130)
                IslandCompactTrailing(state: state, card: card(state, isStale: isStale), isStale: isStale)
                    .padding(.trailing, 18)
            }
            .frame(height: 37)
            .fixedSize()
            .background(.black, in: Capsule())

            IslandMinimal(state: state, card: card(state, isStale: isStale), isStale: isStale)
                .frame(width: 37, height: 37)
                .background(.black, in: Circle())

            VStack(alignment: .leading, spacing: 8) {
                // The header below the camera, as `belowIfTooWide` places it for a long title.
                HStack(alignment: .center, spacing: 12) {
                    IslandExpandedLeading(state: state, card: card(state, isStale: isStale), isStale: isStale)
                    IslandExpandedTrailing(state: state, isStale: isStale)
                        .fixedSize()
                }
                .padding(.top, 30)
                IslandExpandedBottom(
                    state: state, card: card(state, isStale: isStale), hostId: "h_x", link: nil, isStale: isStale
                )
            }
            .padding(.horizontal, 22)
            .padding(.vertical, 18)
            .frame(width: 371, alignment: .leading)
            .background(.black, in: RoundedRectangle(cornerRadius: 44, style: .continuous))
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(wallpaper)
        .environment(\.liveClockFrozenAt, now)
    }
}

private struct WidgetGallery: View {
    let summary: WaitingSummary

    var body: some View {
        VStack(spacing: 24) {
            WaitingSmallView(summary: summary)
                .padding(16)
                .frame(width: 170, height: 170)
                .background(Color.togoInk, in: RoundedRectangle(cornerRadius: 22, style: .continuous))
            HStack(spacing: 20) {
                WaitingRectangularView(summary: summary)
                    .frame(width: 172, height: 76)
                WaitingCircularView(summary: summary)
                    .frame(width: 76, height: 76)
            }
            .foregroundStyle(.white)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(wallpaper)
    }
}

@MainActor
struct LiveActivitySnapshotTests {
    @Test(arguments: cases.indices)
    func lockScreen(index: Int) {
        let item = cases[index]
        assertScreen(named: "lock-\(item.name)") { LockScreenFrame(state: item.state) }
    }

    @Test(arguments: cases.indices)
    func island(index: Int) {
        let item = cases[index]
        assertScreen(named: "island-\(item.name)") { IslandGallery(state: item.state) }
    }

    /// Past the stale date nothing claims to be current: no accent, no running clock.
    @Test func staleNeedsYou() {
        let item = state("needs_you", waiting: 3, ago: 700)
        assertScreen(named: "lock-stale") { LockScreenFrame(state: item, isStale: true) }
        assertScreen(named: "island-stale") { IslandGallery(state: item, isStale: true) }
    }

    @Test(arguments: [0, 1, 5])
    func waitingWidget(count: Int) {
        let oldest = count == 0 ? nil : WaitingSummary.Oldest(bib: "04", agent: "Claude Code", link: nil)
        let summary = WaitingSummary(count: count, oldest: oldest)
        assertScreen(named: "widget-\(count)") { WidgetGallery(summary: summary) }
    }

    @Test func waitingSummaryCountsOpenRequestsOnly() {
        func request(_ id: String, bib: String, ago: TimeInterval, answered: PendingRequest.Answered? = nil) -> PendingRequest {
            PendingRequest(
                requestId: id, hostId: "h_a", sessionId: nil, bib: bib, agent: "Codex", host: "Studio", title: "",
                subject: PushSubject(kind: "command", text: "secret"), options: [],
                receivedAt: now.addingTimeInterval(-ago), answered: answered
            )
        }
        let summary = WaitingSummary(requests: [
            request("r_1", bib: "05", ago: 30),
            request("r_2", bib: "02", ago: 90),
            request("r_3", bib: "01", ago: 120, answered: .approved),
            request("r_4", bib: "09", ago: 900),
        ], now: now, machineName: { _ in "Studio" })
        #expect(summary.count == 2)
        #expect(summary.oldest?.bib == "02")
        #expect(summary.oldest?.agent == "Codex")
        #expect(summary.oldest?.link?.absoluteString == "togo://session/02?host=Studio")
    }
}
