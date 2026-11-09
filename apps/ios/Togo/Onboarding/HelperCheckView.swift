import SwiftUI
import TogoKit
import UIKit

/// What the machine has for Togo. With the helper it is ready to save; without it the
/// user sees what the helper adds, how to install it, and can go on without it.
struct HelperCheckView: View {
    let machine: String
    let report: HelperReport
    let saving: Bool
    let saveError: String?
    let onSave: () -> Void
    let onCheckAgain: () -> Void
    let onUseWithoutHelper: () -> Void

    @State private var copied = false
    @State private var phoneOnTailnet = PhoneTailnet.isConnected()

    var body: some View {
        Group {
            if case .present(let version) = report.helper {
                present(version: version)
            } else {
                missing
            }
        }
        // A failed save's line arrives on `resize`, with the `.error` haptic.
        .resizesOnRows(saveError != nil)
        .haptic(.error, trigger: saveError) { _, new in new != nil }
    }

    /// The helper runs sessions in tmux, so without tmux 3.2 or newer nothing would start:
    /// the screen says so instead of "ready".
    static func needsTmux(_ report: HelperReport) -> Bool {
        !report.tmuxSupported
    }

    private func present(version: String) -> some View {
        let needsTmux = Self.needsTmux(report)
        let away = AwayFromHome(
            machineOnTailnet: report.routes.map { _ in report.machineOnTailnet }, phoneOnTailnet: phoneOnTailnet
        )
        return OnboardingStep(
            title: needsTmux
                ? String(localized: "onboard.helper.needsTmux.title", defaultValue: "\(machine) needs tmux")
                : String(localized: "onboard.helper.ready.title", defaultValue: "\(machine) is ready"),
            message: needsTmux
                ? String(
                    localized: "onboard.helper.needsTmux.message",
                    defaultValue: "The Togo helper is installed, but it starts every session in tmux 3.2 or newer. Install tmux on \(machine), then check again."
                )
                : String(
                    localized: "onboard.helper.ready.message",
                    defaultValue: "The Togo helper is installed, so each agent gets a chat, and you can answer its requests and get notifications."
                )
        ) {
            if needsTmux {
                tmuxInstall
            }
            // A ready Tailscale route is one more check, so it sits in the list at the same
            // spacing; the tip and the steps stay a separate note below it.
            VStack(alignment: .leading, spacing: 14) {
                ReadinessLine(
                    text: String(localized: "onboard.helper.version", defaultValue: "Togo helper \(version)"), ready: true
                )
                machineLines
                if away == .ready {
                    AwayFromHomeNote(machine: machine, state: .ready)
                }
            }
            if let away, away != .ready {
                AwayFromHomeNote(machine: machine, state: away)
            }
            Text(String(
                localized: "onboard.helper.integrations",
                defaultValue: "Next, set up each agent's chat and requests in Settings › Agents › \(machine). One tap sets them all up."
            ))
            .font(.footnote)
            .foregroundStyle(Color.togoMute)
            notes
        } actions: {
            if needsTmux {
                Button(String(localized: "onboard.checkAgain", defaultValue: "Check again"), action: onCheckAgain)
                    .buttonStyle(.primary)
                    .disabled(saving)
                Button(String(localized: "onboard.helper.saveAnyway", defaultValue: "Save and install tmux later"), action: onSave)
                    .buttonStyle(.secondary)
                    .disabled(saving)
            } else {
                Button(String(localized: "onboard.save", defaultValue: "Save"), action: onSave)
                    .buttonStyle(.primary)
                    .disabled(saving)
            }
        }
        .task(id: copied) {
            guard copied else { return }
            try? await Task.sleep(for: .seconds(2))
            copied = false
        }
    }

    /// The command that installs tmux, to copy, or where to get it when there is none.
    @ViewBuilder private var tmuxInstall: some View {
        if let command = HelperProbe.tmuxInstallCommand(os: report.os) {
            VStack(alignment: .leading, spacing: 8) {
                Text(String(localized: "onboard.helper.tmux.install", defaultValue: "To install it, run this on \(machine):"))
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
                CopyableText(text: command)
                copyButton(command)
                if command.hasPrefix("brew") {
                    Text(String(
                        localized: "onboard.helper.tmux.brew",
                        defaultValue: "This uses Homebrew. Without it, install Homebrew from brew.sh first."
                    ))
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
                }
            }
        } else {
            Text(String(
                localized: "onboard.helper.tmux.other",
                defaultValue: "Install tmux 3.2 or newer with \(machine)'s package manager."
            ))
            .font(.subheadline)
            .foregroundStyle(Color.togoMute)
        }
    }

    private func copyButton(_ text: String) -> some View {
        Button {
            UIPasteboard.general.string = text
            copied = true
        } label: {
            Label(
                copied
                    ? String(localized: "onboard.copied", defaultValue: "Copied")
                    : String(localized: "onboard.copyCommand", defaultValue: "Copy command"),
                systemImage: copied ? "checkmark" : "doc.on.doc"
            )
        }
        .buttonStyle(.secondary(.fit))
    }

    private var missing: some View {
        let outdated = if case .outdated = report.helper { true } else { false }
        let install = HelperProbe.installCommand(os: report.os)
        return OnboardingStep(
            title: outdated
                ? String(localized: "onboard.helper.outdated.title", defaultValue: "Update the Togo helper")
                : String(localized: "onboard.helper.missing.title", defaultValue: "Add the Togo helper"),
            message: outdated
                ? String(
                    localized: "onboard.helper.outdated.message",
                    defaultValue: "The helper on \(machine) is too old for this version of Togo. The current one adds:"
                )
                : String(
                    localized: "onboard.helper.missing.message",
                    defaultValue: "A small program on \(machine) that adds:"
                )
        ) {
            VStack(alignment: .leading, spacing: 12) {
                benefit(String(localized: "onboard.helper.adds.status", defaultValue: "A chat for each agent, with its status"), "bubble.left.and.text.bubble.right")
                benefit(String(localized: "onboard.helper.adds.requests", defaultValue: "Approvals from here, the Lock Screen or your Watch"), "checkmark.bubble")
                benefit(String(localized: "onboard.helper.adds.notifications", defaultValue: "Notifications when an agent needs you"), "bell")
            }
            VStack(alignment: .leading, spacing: 8) {
                Text(outdated
                    ? String(localized: "onboard.helper.update", defaultValue: "To update it, run this on \(machine):")
                    : String(localized: "onboard.helper.install", defaultValue: "To install it, run this on \(machine):"))
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
                CopyableText(text: install)
                copyButton(install)
            }
            VStack(alignment: .leading, spacing: 14) { machineLines }
            notes
        } actions: {
            Button(String(localized: "onboard.checkAgain", defaultValue: "Check again"), action: onCheckAgain)
                .buttonStyle(.primary)
                .disabled(saving)
            Button(String(localized: "onboard.helper.skip", defaultValue: "Use without the helper"), action: onUseWithoutHelper)
                .buttonStyle(.secondary)
                .disabled(saving)
        }
        .task(id: copied) {
            guard copied else { return }
            try? await Task.sleep(for: .seconds(2))
            copied = false
        }
    }

    @ViewBuilder private var machineLines: some View {
        ReadinessLine(text: tmuxLine, ready: report.tmuxSupported)
        ReadinessLine(
            text: report.mosh
                ? String(localized: "onboard.helper.mosh.ready", defaultValue: "Mosh ready")
                : String(localized: "onboard.helper.mosh.missing", defaultValue: "Mosh not installed; SSH works"),
            ready: report.mosh
        )
    }

    @ViewBuilder private var notes: some View {
        if report.tailscaleSSH {
            Text(String(
                localized: "onboard.helper.tailscaleSSH", defaultValue: "Tailscale SSH is handling sign-in for this machine."
            ))
            .font(.footnote)
            .foregroundStyle(Color.togoMute)
        }
        if let saveError {
            Label(saveError, systemImage: "exclamationmark.circle")
                .font(.footnote)
                .foregroundStyle(Color.togoSnow)
                // motion: preventing a jump — the list opens a row for the failed save, in place
                .transition(.opacity)
        }
    }

    private var tmuxLine: String {
        guard let tmux = report.tmux else {
            return String(localized: "onboard.helper.tmux.missing", defaultValue: "tmux isn't installed. The helper needs 3.2 or newer.")
        }
        if report.tmuxSupported { return tmux }
        return String(localized: "onboard.helper.tmux.old", defaultValue: "\(tmux) is too old; Togo needs 3.2 or newer")
    }

    private func benefit(_ text: String, _ systemImage: String) -> some View {
        Label {
            Text(text)
                .font(.body)
                .foregroundStyle(Color.togoSnow)
        } icon: {
            Image(systemName: systemImage)
                .foregroundStyle(Color.togoMute)
        }
    }
}
