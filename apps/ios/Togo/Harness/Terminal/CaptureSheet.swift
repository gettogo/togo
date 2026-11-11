import SwiftUI
import TogoKit
import UIKit

/// What the capture sheet shows: the pane's history read from the machine, opened at a line.
struct CaptureRequest: Identifiable, Equatable {
    let id = UUID()
    var hostID: HostID
    var sessionID: String
    var needle: String?
}

/// The pane's history from the machine, for "Show in terminal" when the phone does not hold the
/// line (Mosh keeps no scrollback; a fresh attach has little). It opens at the last line holding
/// the needle, marked, and VoiceOver starts there.
struct CaptureSheet: View {
    @Environment(\.dismiss) private var dismiss
    let request: CaptureRequest
    let services: any AppServices

    /// How much history to read: enough to reach well past a screen or two of agent output.
    static let lines = 3000

    private enum Phase: Equatable {
        case loading
        case loaded([String], marked: Int?)
        case failed
    }

    @Environment(\.accessibilityVoiceOverEnabled) private var voiceOver
    @State private var phase: Phase = .loading
    @State private var attempt = 0
    @AccessibilityFocusState private var focusMarked: Bool

    var body: some View {
        NavigationStack {
            content
                .navigationTitle(TerminalSyncCopy.fromMachine)
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button(TerminalSyncCopy.done, systemImage: "checkmark", role: .confirm) { dismiss() }
                    }
                    if case .loaded(let lines, _) = phase {
                        ToolbarItem(placement: .bottomBar) {
                            Button(TerminalSyncCopy.copyAll, systemImage: "doc.on.doc") {
                                UIPasteboard.general.string = lines.joined(separator: "\n")
                                Haptic.done.play()
                            }
                        }
                    }
                }
        }
        .presentationDetents([.large])
        .presentationBackground(Color.togoInk)
        .task(id: attempt) { await load() }
    }

    /// Loading, failed and loaded crossfade in place in the sheet.
    private var content: some View {
        ZStack {
            phaseView
                .id(phaseKind)
        }
        // motion: state change — reading gives way to the history (or why it failed) in place.
        .motion(.fade, value: phaseKind)
    }

    private var phaseKind: Int {
        switch phase {
        case .loading: 0
        case .failed: 1
        case .loaded: 2
        }
    }

    @ViewBuilder private var phaseView: some View {
        switch phase {
        case .loading:
            ProgressView(TerminalSyncCopy.loading)
                .foregroundStyle(Color.togoMute)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed:
            ContentUnavailableView {
                Label(TerminalSyncCopy.captureFailed, systemImage: "terminal")
            } actions: {
                Button(TerminalSyncCopy.tryAgain) { attempt += 1 }
            }
            .foregroundStyle(Color.togoMute)
        case .loaded(let lines, let marked):
            history(lines, marked: marked)
        }
    }

    /// One text, so a long press picks a word and its handles reach across lines and screens of
    /// history; the needle's line is marked and in the middle. VoiceOver keeps a line per
    /// element (`lineList`), so it can start on the marked line.
    @ViewBuilder private func history(_ lines: [String], marked: Int?) -> some View {
        if voiceOver {
            lineList(lines, marked: marked)
        } else {
            let text = CaptureText(lines: lines, marked: marked)
            SelectableText(
                key: text, layout: .scrolls, insets: UIEdgeInsets(top: 16, left: 16, bottom: 16, right: 16),
                reveal: text.markedRange, copy: { ChatPasteboard.copy(exact: $0.string) }
            ) { traits in
                text.attributed(traits: traits)
            }
        }
    }

    private func lineList(_ lines: [String], marked: Int?) -> some View {
        ScrollViewReader { proxy in
            ScrollView([.vertical]) {
                LazyVStack(alignment: .leading, spacing: 0) {
                    ForEach(lines.indices, id: \.self) { index in
                        line(lines[index], marked: index == marked)
                            .id(index)
                    }
                }
                .padding(16)
            }
            .onAppear {
                guard let marked else { return }
                proxy.scrollTo(marked, anchor: .center)
                focusMarked = true
            }
        }
    }

    @ViewBuilder private func line(_ text: String, marked: Bool) -> some View {
        let row = Text(verbatim: text.isEmpty ? " " : text)
            .font(.mono(.footnote))
            .foregroundStyle(Color.togoSnow.opacity(marked ? 1 : 0.85))
            .textSelection(.enabled)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.vertical, 1)
        if marked {
            row
                .padding(.horizontal, 6)
                .background(Color.togoSnow.opacity(0.14), in: .rect(cornerRadius: 4, style: .continuous))
                .overlay(alignment: .leading) {
                    Rectangle().fill(Color.togoSnow.opacity(0.7)).frame(width: 2)
                }
                .accessibilityLabel(Text(verbatim: text))
                .accessibilityValue(TerminalSyncCopy.marked)
                .accessibilityFocused($focusMarked)
        } else {
            row
        }
    }

    private func load() async {
        phase = .loading
        do {
            let text = try await services.capture(hostID: request.hostID, sessionID: request.sessionID, lines: Self.lines)
            let lines = text.split(separator: "\n", omittingEmptySubsequences: false).map(String.init)
            phase = .loaded(lines, marked: JumpLanding.captureLine(needle: request.needle, lines: lines))
        } catch is CancellationError {
            return
        } catch {
            phase = .failed
        }
    }
}

/// The captured history as one selectable text: SF Mono at the footnote size, the marked line
/// brighter on a faint band, the rest a little dimmer, as the line list draws them.
struct CaptureText: Hashable {
    let lines: [String]
    let marked: Int?

    var string: String { lines.joined(separator: "\n") }

    /// The marked line's characters in the joined text, in UTF-16 as UIKit counts them.
    var markedRange: NSRange? {
        guard let marked, lines.indices.contains(marked) else { return nil }
        let before = lines[..<marked].reduce(0) { $0 + ($1 as NSString).length + 1 }
        return NSRange(location: before, length: (lines[marked] as NSString).length)
    }

    @MainActor func attributed(traits: UITraitCollection) -> NSAttributedString {
        let style = ChatSelectable.Style(textStyle: .footnote, monospaced: true, color: UIColor.togoSnow.withAlphaComponent(0.85))
        let out = ChatSelectable.attributed(AttributedString(string), style: style, traits: traits)
        if let range = markedRange {
            out.addAttributes([
                .foregroundColor: UIColor.togoSnow,
                .backgroundColor: UIColor.togoSnow.withAlphaComponent(0.14),
            ], range: range)
        }
        return out
    }
}
