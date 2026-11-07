import SwiftUI
import TogoKit
import UIKit

/// A shell command as a block: the command line in SF Mono with how it ended, the tail of its
/// output underneath, and Copy output, Rerun and Collapse. Tapping the command line collapses
/// or opens the output. Long output starts collapsed.
struct CommandBlockView: View {
    let item: HarnessItem
    let store: HarnessSessionStore

    /// The user's choice; nil keeps the block's default.
    @State private var folded: Bool?
    @State private var failure: String?

    var body: some View {
        if let block = CommandBlock(item) {
            content(block)
        }
    }

    private func content(_ block: CommandBlock) -> some View {
        let collapsed = folded ?? block.startsCollapsed
        return VStack(alignment: .leading, spacing: 0) {
            header(block, collapsed: collapsed)
            if block.hasOutput {
                if collapsed {
                    Text(verbatim: TerminalSyncCopy.lines(block.lineCount))
                        .font(.mono(.caption))
                        .foregroundStyle(Color.togoMute)
                        .padding(.horizontal, 12)
                        .padding(.bottom, 10)
                        .transition(.opacity)
                } else {
                    Rectangle().fill(Color.togoLine).frame(height: 1)
                    Text(verbatim: block.output)
                        .font(.mono(.caption))
                        .foregroundStyle(Color.togoSnow.opacity(0.85))
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(12)
                        .transition(.opacity)
                }
            }
            if !block.isRunning {
                actionBar(block, collapsed: collapsed)
            }
        }
        .background(Color.togoDeepest, in: .rect(cornerRadius: 12, style: .continuous))
        .motion(.collapse, value: collapsed)
        .contextMenu { menu(block, collapsed: collapsed) }
        .accessibilityElement(children: .contain)
    }

    private func header(_ block: CommandBlock, collapsed: Bool) -> some View {
        Button { toggle(block) } label: {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                statusGlyph(block)
                Text(verbatim: block.title)
                    .foregroundStyle(Color.togoSnow)
                    .lineLimit(3)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if !block.badge.isEmpty {
                    Text(verbatim: block.badge)
                        .foregroundStyle(Color.togoMute)
                }
                if block.hasOutput {
                    Image(systemName: collapsed ? "chevron.down" : "chevron.up")
                        .font(.caption2.weight(.semibold))
                        .foregroundStyle(Color.togoMute)
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

    @ViewBuilder private func statusGlyph(_ block: CommandBlock) -> some View {
        switch block.status {
        case .running:
            ProgressView().controlSize(.mini)
        case .succeeded:
            Image(systemName: "checkmark").foregroundStyle(PrimitiveTint.ok).accessibilityHidden(true)
        case .failed:
            Image(systemName: "xmark").foregroundStyle(PrimitiveTint.fail).accessibilityHidden(true)
        case .finished:
            Text(verbatim: "$").foregroundStyle(Color.togoMute).accessibilityHidden(true)
        }
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
                    .lineLimit(2)
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
        folded = !(folded ?? block.startsCollapsed)
    }

    private func copy(_ text: String) {
        UIPasteboard.general.string = text
        Haptic.done.play()
        AccessibilityNotification.Announcement(TerminalSyncCopy.copied).post()
    }

    private func rerun(_ block: CommandBlock) {
        failure = nil
        Task {
            do {
                try await store.send(block.command, mode: .now)
                Haptic.done.play()
            } catch {
                failure = TerminalSyncCopy.rerunFailed
                Haptic.error.play()
            }
        }
    }
}
