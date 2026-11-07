import SwiftUI
import TogoPush
import WatchKit

/// One request filling the screen: beacon, mono bib and agent, the question, the command in
/// a mono well, Yes and No, and Reply where the Watch can send it.
struct RequestDetailView: View {
    let store: WatchStore
    let requestId: String
    var position = 1
    var count = 1
    @Environment(\.dismiss) private var dismiss
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        if let request = store.request(requestId) {
            content(request)
        } else {
            AllClearFallback()
        }
    }

    private func content(_ request: PendingRequest) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 8) {
                HStack(spacing: 6) {
                    BeaconDot(lit: true)
                    RequestHeader(request: request)
                    Spacer(minLength: 4)
                    if let counter = WatchHome.counter(position: position, count: count) {
                        Text(counter)
                            .font(.system(.footnote, design: .monospaced))
                            .foregroundStyle(.secondary)
                            // trunc-ok: "1 of 3"
                            .lineLimit(1)
                            .contentTransition(reduceMotion ? .identity : .numericText(value: Double(count)))
                            .transition(.opacity)
                    }
                }
                .motion(.respond, value: count)
                Text(request.title)
                    .font(.headline)
                    .fixedSize(horizontal: false, vertical: true)
                if let command = WatchHome.commandText(request) {
                    Text(command)
                        .font(.system(.footnote, design: .monospaced))
                        .fixedSize(horizontal: false, vertical: true)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(8)
                        .background(Color.black.opacity(0.5), in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                }
                AnswerButtons(store: store, request: request, done: dismissSoon)
                    .padding(.top, 2)
                if store.canReply(request.requestId) {
                    ReplyLink(
                        store: store, target: .answer(hostId: request.hostId, requestId: request.requestId),
                        title: [request.bib, request.agent.map(AgentName.display)].compactMap { $0 }.joined(separator: " ")
                    )
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    /// Leaves the answered request on screen long enough to read the result.
    private func dismissSoon() {
        Task {
            try? await Task.sleep(for: .seconds(1.2))
            dismiss()
        }
    }
}

private struct AnswerButtons: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let store: WatchStore
    let request: PendingRequest
    let done: () -> Void

    private var outcome: WatchStore.Outcome? { store.outcome(request.requestId) }
    /// Off for a moment after the request shows; see `WatchHome.armDelay`.
    @State private var armed = false
    /// The state on screen. It follows the store's in one transaction, so the buttons give way
    /// to the result, and a failure line arrives, with the screen around them moving too.
    @State private var shown: WatchStore.RequestState?
    private var canApprove: Bool { WatchStore.canApprove(request) }

    var body: some View {
        // The ten-minute limit passes while the screen is open; the state is read again often.
        TimelineView(.periodic(from: .now, by: 5)) { timeline in
            // The revision is read so a refused tap redraws at once.
            let _ = store.revision
            let state = store.state(of: request.requestId, at: timeline.date)
            VStack(spacing: 6) {
                switch shown ?? state {
                case .approved:
                    result(WatchCopy.approved, systemImage: "checkmark")
                case .denied:
                    result(WatchCopy.denied, systemImage: "xmark")
                case .answered:
                    result(WatchCopy.answered, systemImage: "checkmark.circle")
                case .answeredElsewhere:
                    result(WatchCopy.answeredElsewhere, systemImage: "checkmark.circle")
                case .expired:
                    result(WatchCopy.expired, systemImage: "clock")
                case .open, .sending, .failed, .notSetUp:
                    buttons(shown ?? state)
                }
            }
            .onChange(of: state, initial: true) { _, new in follow(new) }
        }
        .task {
            try? await Task.sleep(for: WatchHome.armDelay)
            armed = true
        }
        .onChange(of: outcome) { _, new in
            switch new {
            case .approved:
                WKInterfaceDevice.current().play(.success)
                done()
            case .denied:
                WKInterfaceDevice.current().play(.directionDown)
                done()
            case .failed, .notSetUp:
                WKInterfaceDevice.current().play(.failure)
            case .sending, nil:
                break
            }
        }
    }

    /// A result presents (`dropIn`); a change among the buttons' own states, such as sending or
    /// a failure line, responds in place.
    private func follow(_ new: WatchStore.RequestState) {
        guard let old = shown else {
            shown = new
            return
        }
        guard old != new else { return }
        let role: MotionRole = new.isResult && !old.isResult ? .dropIn : .respond
        withAnimation(Motion.animation(role, reduce: reduceMotion)) { shown = new }
    }

    @ViewBuilder private func buttons(_ state: WatchStore.RequestState) -> some View {
        let sending = state == .sending
        if state == .failed {
            Text(WatchCopy.sendFailed)
                .font(.footnote)
                .frame(maxWidth: .infinity, alignment: .leading)
                .transition(Motion.revealConceal(reduce: reduceMotion))
        } else if state == .notSetUp {
            Text(WatchCopy.notSetUp(machine: request.host.map { PromptText.clean($0, maxLines: 1) }))
                .font(.footnote)
                .frame(maxWidth: .infinity, alignment: .leading)
                .transition(Motion.revealConceal(reduce: reduceMotion))
        }
        Button {
            guard armed else { return }
            Task { await store.answer(request.requestId, "approve") }
        } label: {
            Text(WatchCopy.yes)
                .frame(maxWidth: .infinity)
                .foregroundStyle(canApprove ? Color.black : Color.secondary)
        }
        .buttonStyle(.borderedProminent)
        .buttonBorderShape(.capsule)
        .tint(.togoAccent)
        .disabled(!canApprove || sending)
        .transition(Motion.swap(reduce: reduceMotion))
        if !canApprove {
            Text(WatchCopy.answerOnPhone)
                .font(.footnote)
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        Button {
            guard armed else { return }
            Task { await store.answer(request.requestId, "deny") }
        } label: {
            Text(WatchCopy.no)
                .frame(maxWidth: .infinity)
        }
        .buttonStyle(.plain)
        .frame(minHeight: 36)
        .disabled(sending)
        .transition(Motion.swap(reduce: reduceMotion))
    }

    private func result(_ title: String, systemImage: String) -> some View {
        Label(title, systemImage: systemImage)
            .font(.body.weight(.semibold))
            .frame(maxWidth: .infinity, minHeight: 44)
            .background(.quaternary, in: .capsule)
            .transition(Motion.swap(reduce: reduceMotion))
    }
}

private extension WatchStore.RequestState {
    /// The request is settled and its result line takes the buttons' place.
    var isResult: Bool {
        switch self {
        case .approved, .denied, .answered, .answeredElsewhere, .expired: true
        case .open, .sending, .failed, .notSetUp: false
        }
    }
}

/// What shows when a request was answered and nothing else waits.
private struct AllClearFallback: View {
    var body: some View {
        HStack(spacing: 6) {
            BeaconDot(lit: false)
            Text(WatchCopy.allClear)
                .font(.footnote)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

extension Color {
    /// The beacon blue. The Watch is always dark, so this is the accent's dark value.
    static let togoAccent = Color(red: 0x9F / 255, green: 0xD0 / 255, blue: 0xF5 / 255)
}

/// "04 Claude Code": the bib in mono, then the agent.
struct RequestHeader: View {
    let request: PendingRequest

    var body: some View {
        HStack(spacing: 6) {
            if let bib = request.bib, !bib.isEmpty {
                Text(PromptText.clean(bib, maxLines: 1))
                    .font(.system(.footnote, design: .monospaced))
                    // trunc-ok: a bib is two or three digits
                    .lineLimit(1)
            }
            if let agent = request.agent, !agent.isEmpty {
                Text(AgentName.display(agent))
                    .font(.footnote)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .foregroundStyle(.secondary)
    }
}
