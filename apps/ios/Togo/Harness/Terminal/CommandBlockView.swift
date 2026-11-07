import SwiftUI
import TogoKit
import UIKit

/// A shell command as a block: the command line in SF Mono with how it ended, the tail of its
/// output underneath, and Copy output, Rerun and Collapse. Tapping the command line collapses
/// or opens the output. Long output starts collapsed.
///
/// Motion: the output unfolds and closes as a two-phase disclosure on `resize`, so the chat rows
/// below move with the block and the output never draws outside it; the action bar and a
/// failure are simply there when they happen; the status glyph crossfades in a fixed slot.
struct CommandBlockView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let item: HarnessItem
    let store: HarnessSessionStore

    /// The user's choice; nil keeps the block's default.
    @State private var output: Disclosure?
    @State private var failure: String?
    /// Mirrors "the command ended", so the action bar arrives in a transaction.
    @State private var ended: Bool?

    var body: some View {
        if let block = CommandBlock(item) {
            content(block)
        }
    }

    private func disclosure(_ block: CommandBlock) -> Disclosure {
        output ?? Disclosure(open: !block.startsCollapsed)
    }

    private func content(_ block: CommandBlock) -> some View {
        let shown = disclosure(block)
        let collapsed = !shown.isOpen
        return VStack(alignment: .leading, spacing: 0) {
            header(block, collapsed: collapsed)
            if block.hasOutput {
                if shown.open {
                    VStack(alignment: .leading, spacing: 0) {
                        Rectangle().fill(Color.togoLine).frame(height: 1)
                        Text(verbatim: block.output)
                            .font(.mono(.caption))
                            .foregroundStyle(Color.togoSnow.opacity(0.85))
                            .textSelection(.enabled)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(12)
                    }
                    // The block's growing edge uncovers the output in place.
                    .unfolds(shown)
                } else {
                    Text(verbatim: TerminalSyncCopy.lines(block.lineCount))
                        .font(.mono(.caption))
                        .foregroundStyle(Color.togoMute)
                        .padding(.horizontal, 12)
                        .padding(.bottom, 10)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
            if ended ?? !block.isRunning {
                actionBar(block, collapsed: collapsed)
            }
        }
        .background(Color.togoDeepest, in: .rect(cornerRadius: 12, style: .continuous))
        // Nothing draws outside the block while it grows or shrinks.
        .clipShape(.rect(cornerRadius: 12, style: .continuous))
        .contextMenu { menu(block, collapsed: collapsed) }
        .accessibilityElement(children: .contain)
        .onAppear { if ended == nil { ended = !block.isRunning } }
        .onChange(of: block.isRunning) { _, running in
            // The command ending is a state change: the action bar is simply there.
            withTransaction(Transaction(animation: nil)) { ended = !running }
        }
    }

    private func header(_ block: CommandBlock, collapsed: Bool) -> some View {
        Button { toggle(block) } label: {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                statusGlyph(block)
                // trunc-ok: a collapsed block keeps three lines of its command; opening it shows all
                Text(verbatim: block.title)
                    .foregroundStyle(Color.togoSnow)
                    .lineLimit(collapsed ? 3 : nil)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if !block.badge.isEmpty {
                    Text(verbatim: block.badge)
                        .foregroundStyle(Color.togoMute)
                }
                if block.hasOutput {
                    Image(systemName: collapsed ? "chevron.down" : "chevron.up")
                        .font(.caption2.weight(.semibold))
                        .foregroundStyle(Color.togoMute)
                        .contentTransition(reduceMotion ? .identity : .symbolEffect(.replace))
                        // motion: cause → effect — the chevron flips as the person opens or closes the output.
                        .motion(.respond, value: collapsed)
                        .accessibilityHidden(true)
                }
            }
            .font(.mono(.footnote))
            .padding(12)
            .frame(minHeight: 44)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(block.spoken)
        .accessibilityValue(block.hasOutput ? (collapsed ? ChatCopy.collapsed : ChatCopy.expanded) : "")
        .accessibilityActions { menu(block, collapsed: collapsed) }
    }

    /// Running → how it ended, crossfading in a fixed slot so the command never shifts.
    private func statusGlyph(_ block: CommandBlock) -> some View {
        ZStack {
            switch block.status {
            case .running:
                ProgressView().controlSize(.mini)
            case .succeeded:
                Image(systemName: "checkmark").foregroundStyle(PrimitiveTint.ok)
            case .failed:
                Image(systemName: "xmark").foregroundStyle(PrimitiveTint.fail)
            case .finished:
                Text(verbatim: "$").foregroundStyle(Color.togoMute)
            }
        }
        .slot(width: 16, key: block.status)
        .accessibilityHidden(true)
    }

    private func actionBar(_ block: CommandBlock, collapsed: Bool) -> some View {
        HStack(spacing: 4) {
            Button(TerminalSyncCopy.copyOutput, systemImage: "doc.on.doc") { copy(block.copyText) }
            if block.canRerun {
                Button(TerminalSyncCopy.rerun, systemImage: "arrow.clockwise") { rerun(block) }
            }
            if let failure {
                Text(failure)
                    .foregroundStyle(Color.togoMute)
            }
            Spacer(minLength: 0)
        }
        .font(.footnote)
        .labelStyle(.titleAndIcon)
        .buttonStyle(.borderless)
        .tint(Color.togoMute)
        .padding(.horizontal, 6)
        .padding(.bottom, 4)
        .accessibilityHidden(true)
    }

    @ViewBuilder private func menu(_ block: CommandBlock, collapsed: Bool) -> some View {
        Button(TerminalSyncCopy.copyOutput, systemImage: "doc.on.doc") { copy(block.copyText) }
        if !block.command.isEmpty {
            Button(TerminalSyncCopy.copyCommand, systemImage: "terminal") { copy(block.command) }
        }
        if block.canRerun {
            Button(TerminalSyncCopy.rerun, systemImage: "arrow.clockwise") { rerun(block) }
        }
        if block.hasOutput {
            Button(
                collapsed ? TerminalSyncCopy.expand : TerminalSyncCopy.collapse,
                systemImage: collapsed ? "chevron.down" : "chevron.up"
            ) { toggle(block) }
        }
        if item.anchor != nil, store.has(HarnessCapability.anchors) {
            Button(ChatCopy.showInTerminal, systemImage: "arrow.up.forward.app") {
                Task { await store.showInTerminal(item) }
            }
        }
    }

    private func toggle(_ block: CommandBlock) {
        guard block.hasOutput else { return }
        if output == nil { output = disclosure(block) }
        let binding = Binding { output ?? Disclosure() } set: { output = $0 }
        Motion.toggle(binding, reduce: reduceMotion)
    }

    private func copy(_ text: String) {
        UIPasteboard.general.string = text
        Haptic.done.play()
        AccessibilityNotification.Announcement(TerminalSyncCopy.copied).post()
    }

    private func rerun(_ block: CommandBlock) {
        failure = nil
        Task { await sendAgain(block) }
    }

    private func sendAgain(_ block: CommandBlock) async {
        do {
            try await store.send(block.command, mode: .now)
            Haptic.done.play()
        } catch {
            // Instant, at the action that failed, with the error haptic.
            Haptic.error.play()
            failure = TerminalSyncCopy.rerunFailed
        }
    }
}
