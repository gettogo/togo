import SwiftUI
import TogoPush
import WatchKit

/// One request: the question, what it is about, the machine, and Approve and Deny.
struct RequestDetailView: View {
    let store: WatchStore
    let requestId: String
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        if let request = store.request(requestId) {
            content(request)
        } else {
            ContentUnavailableView(WatchCopy.nothingWaiting, systemImage: "checkmark.circle")
        }
    }

    private func content(_ request: PendingRequest) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 10) {
                RequestHeader(request: request)
                Text(request.title)
                    .font(.headline)
                    .fixedSize(horizontal: false, vertical: true)
                if let subject = request.subject, subject.isValid {
                    Text(PromptText.clean(subject.text))
                        .font(.system(.footnote, design: .monospaced))
                        .fixedSize(horizontal: false, vertical: true)
                }
                if let host = request.host, !host.isEmpty {
                    Label(PromptText.clean(host, maxLines: 1), systemImage: "desktopcomputer")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                }
                AnswerButtons(store: store, request: request, done: dismissSoon)
                    .padding(.top, 4)
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
    let store: WatchStore
    let request: PendingRequest
    let done: () -> Void

    private var outcome: WatchStore.Outcome? { store.outcome(request.requestId) }
    private var canApprove: Bool { WatchStore.canApprove(request) }

    var body: some View {
        // The ten-minute limit passes while the screen is open; the state is read again often.
        TimelineView(.periodic(from: .now, by: 5)) { timeline in
            // The revision is read so a refused tap redraws at once.
            let _ = store.revision
            let state = store.state(of: request.requestId, at: timeline.date)
            VStack(spacing: 6) {
                switch state {
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
                    buttons(state)
                }
            }
            .animation(.snappy, value: state)
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

    @ViewBuilder private func buttons(_ state: WatchStore.RequestState) -> some View {
        let sending = state == .sending
        if state == .failed {
            Text(WatchCopy.sendFailed)
                .font(.footnote)
                .frame(maxWidth: .infinity, alignment: .leading)
        } else if state == .notSetUp {
            Text(WatchCopy.notSetUp(machine: request.host.map { PromptText.clean($0, maxLines: 1) }))
                .font(.footnote)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        Button {
            Task { await store.answer(request.requestId, "approve") }
        } label: {
            Text(WatchCopy.approve)
                .frame(maxWidth: .infinity)
                .foregroundStyle(canApprove ? Color.black : Color.secondary)
        }
        .buttonStyle(.borderedProminent)
        .tint(.togoAccent)
        .disabled(!canApprove || sending)
        if !canApprove {
            Text(WatchCopy.answerOnPhone)
                .font(.footnote)
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        Button(role: .destructive) {
            Task { await store.answer(request.requestId, "deny") }
        } label: {
            Text(WatchCopy.deny)
                .frame(maxWidth: .infinity)
        }
        .disabled(sending)
    }

    private func result(_ title: String, systemImage: String) -> some View {
        Label(title, systemImage: systemImage)
            .font(.body.weight(.semibold))
            .frame(maxWidth: .infinity, minHeight: 44)
            .background(.quaternary, in: .capsule)
            .transition(.opacity.combined(with: .scale(scale: 0.96)))
    }
}

extension Color {
    /// The approve color. The Watch is always dark, so this is the accent's dark value.
    static let togoAccent = Color(red: 0x9F / 255, green: 0xD0 / 255, blue: 0xF5 / 255)
}
