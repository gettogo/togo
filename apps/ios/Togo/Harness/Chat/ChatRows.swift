import SwiftUI
import TogoKit

/// The user's prompt: plain text on a raised bubble at the trailing edge. A queued one is
/// dimmed and says so.
struct PromptBubble: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let text: String
    var note: String?
    var dimmed = false

    var body: some View {
        VStack(alignment: .trailing, spacing: 6) {
            // A prompt of pictures alone has no words to set in a bubble.
            if !text.isEmpty {
                Text(text)
                    .font(.body)
                    .foregroundStyle(Color.togoSnow)
                    .textSelection(.enabled)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 10)
                    .background(Color.togoRaised, in: .rect(cornerRadius: 14, style: .continuous))
                    .opacity(dimmed ? 0.6 : 1)
                    .motion(.respond, value: dimmed)
            }
            if let note {
                Text(note)
                    .font(.caption)
                    .foregroundStyle(Color.togoMute)
                    .contentTransition(.interpolate)
                    .transition(Motion.revealConceal(reduce: reduceMotion))
            }
        }
        .padding(.leading, 48)
        .frame(maxWidth: .infinity, alignment: .trailing)
    }
}

/// A prompt the phone still holds: waiting for the turn, on its way, or refused. Waiting and
/// refused ones can be taken back; a refused one can go again.
///
/// Its state changes in the store's arrival transaction (`resize`): Try again reveals, the note
/// crossfades, and a refusal plays the error haptic once.
struct PendingPromptRow: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let prompt: PendingPrompt
    let onCancel: () -> Void
    let onRetry: () -> Void

    var body: some View {
        VStack(alignment: .trailing, spacing: 2) {
            if !prompt.images.isEmpty {
                ChatImageGrid(images: prompt.images.map { HarnessImage(path: $0) }, alignment: .trailing)
                    .padding(.leading, 48)
                    .padding(.bottom, 4)
                    .opacity(0.6)
            }
            PromptBubble(text: prompt.text, note: note, dimmed: true)
            HStack(spacing: 4) {
                if failed {
                    Button(ChatCopy.tryAgain, action: onRetry)
                        .buttonStyle(PlainAnswerStyle())
                        .transition(Motion.revealConceal(reduce: reduceMotion))
                }
                if prompt.state != .sending {
                    Button(ChatCopy.cancel, action: onCancel)
                        .buttonStyle(PlainAnswerStyle())
                        .transition(Motion.revealConceal(reduce: reduceMotion))
                }
            }
            .font(.footnote)
        }
        .haptic(.error, trigger: failed) { _, failed in failed }
        .accessibilityElement(children: .combine)
        .accessibilityLabel(ChatCopy.spokenPrompt(prompt.text) + ", " + (note ?? ""))
        .accessibilityActions {
            if case .failed = prompt.state { Button(ChatCopy.tryAgain, action: onRetry) }
            if prompt.state != .sending { Button(ChatCopy.cancel, action: onCancel) }
        }
    }

    private var failed: Bool {
        if case .failed = prompt.state { true } else { false }
    }

    private var note: String? {
        switch prompt.state {
        case .waiting: ChatCopy.queued
        case .sending: ChatCopy.sending
        case .failed(let message): ChatCopy.notSent + ". " + message
        }
    }
}

/// The agent's answer, in full.
struct ReplyRow: View, Equatable {
    let text: String

    var body: some View {
        MarkdownText(text: text)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// A quiet system line: compacted, model changed, interrupted.
struct ChatNoticeRow: View {
    let notice: HarnessItem.Notice

    var body: some View {
        Label(notice.text, systemImage: symbol)
            .font(.footnote)
            .foregroundStyle(Color.togoMute)
            .frame(maxWidth: .infinity, alignment: .center)
            .multilineTextAlignment(.center)
    }

    private var symbol: String {
        switch notice.code {
        case "compacted": "rectangle.compress.vertical"
        case "model", "mode": "arrow.triangle.2.circlepath"
        case "rate_limit": "hourglass"
        case "interrupted": "stop.circle"
        case "resumed": "arrow.uturn.forward"
        case "fallback": "terminal"
        default: notice.level == "info" ? "info.circle" : "exclamationmark.circle"
        }
    }
}

extension HarnessItem {
    /// The words a row copies or quotes.
    var chatText: String? {
        switch body {
        case .user(let user): user.text
        case .assistant(let text), .thinking(let text): text.text
        case .notice(let notice): notice.text
        case .shell(let shell): shell.command
        case .tool(let tool): tool.subject?.text ?? tool.title
        case .question(let question): question.questions.map(\.prompt).joined(separator: "\n")
        default: nil
        }
    }
}

/// "> " before every line, for quoting in a reply.
enum ChatQuote {
    static func quoted(_ text: String) -> String {
        text.split(separator: "\n", omittingEmptySubsequences: false).map { "> " + $0 }.joined(separator: "\n") + "\n"
    }
}

/// A turn's work line in the chat, with the pictures its tools read or made under it. The
/// pictures reveal in a `resize` transaction when the first one arrives (spec §2.1).
struct ChatWorkRow<Menu: View>: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let line: WorkLine
    let pictures: [HarnessImage]
    @ViewBuilder let menu: () -> Menu

    @State private var showsPictures: Bool?

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            line
                .contextMenu { menu() }
            if showsPictures ?? !pictures.isEmpty, !pictures.isEmpty {
                ChatImageGrid(images: pictures)
                    .padding(.leading, 22)
                    .transition(Motion.revealConceal(reduce: reduceMotion))
            }
        }
        .onAppear { if showsPictures == nil { showsPictures = !pictures.isEmpty } }
        .onChange(of: pictures.isEmpty) { _, empty in
            withAnimation(Motion.animation(.resize, reduce: reduceMotion)) { showsPictures = !empty }
        }
    }
}
