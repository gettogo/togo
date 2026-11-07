import SwiftUI
import TogoKit
import UIKit

/// The kind of machine the steps are written for.
enum MachineOS: String, CaseIterable, Sendable {
    case macOS, linux

    var name: String {
        switch self {
        case .macOS: "macOS"
        case .linux: "Linux"
        }
    }

    /// What `uname -s` prints, which is what `HelperProbe` keys its commands on.
    var unameName: String {
        switch self {
        case .macOS: "Darwin"
        case .linux: "Linux"
        }
    }
}

/// One instruction: a sentence, and the command it refers to when there is one.
struct SetupStep: Equatable, Sendable {
    var text: String
    var command: String?
}

extension SetupItem {
    var title: String {
        switch self {
        case .remoteLogin: String(localized: "tutorial.setup.remoteLogin", defaultValue: "Turn on Remote Login (SSH)")
        case .reachable: String(localized: "tutorial.setup.reachable", defaultValue: "Make sure your device can reach it")
        case .phoneKey: String(localized: "tutorial.setup.phoneKey", defaultValue: "Add this device's key")
        case .mosh: String(localized: "tutorial.setup.mosh", defaultValue: "Install Mosh")
        case .helper: String(localized: "tutorial.setup.helper", defaultValue: "Install the Togo helper")
        }
    }

    /// Every step runs on the machine itself; the phone never copies a program to it.
    func steps(for os: MachineOS) -> [SetupStep] {
        switch (self, os) {
        case (.remoteLogin, .macOS):
            [SetupStep(text: String(
                localized: "tutorial.setup.remoteLogin.mac",
                defaultValue: "Open System Settings > General > Sharing and turn on Remote Login."
            ))]
        case (.remoteLogin, .linux):
            // Ubuntu Desktop and Arch ship without an SSH server, so each line installs it first.
            [
                SetupStep(
                    text: String(
                        localized: "tutorial.setup.remoteLogin.debian",
                        defaultValue: "Install the SSH server and start it. On Debian or Ubuntu:"
                    ),
                    command: "sudo apt install openssh-server && sudo systemctl enable --now ssh"
                ),
                SetupStep(
                    text: String(localized: "tutorial.setup.remoteLogin.fedora", defaultValue: "On Fedora:"),
                    command: "sudo dnf install openssh-server && sudo systemctl enable --now sshd"
                ),
                SetupStep(
                    text: String(localized: "tutorial.setup.remoteLogin.arch", defaultValue: "On Arch:"),
                    command: "sudo pacman -S openssh && sudo systemctl enable --now sshd"
                ),
            ]
        case (.reachable, _):
            [
                SetupStep(text: String(
                    localized: "tutorial.setup.reachable.local",
                    defaultValue: "On the same Wi-Fi, the machine's local name or address works."
                ), command: os == .macOS ? #"echo "$(scutil --get LocalHostName).local""# : "hostname"),
                SetupStep(text: String(
                    localized: "tutorial.setup.reachable.away",
                    defaultValue: "Away from that network, put the machine and this device on the same Tailscale network, or use a public address that forwards to its SSH port."
                )),
            ]
        case (.phoneKey, _):
            [SetupStep(text: String(
                localized: "tutorial.setup.phoneKey.steps",
                defaultValue: "When you add a machine, Togo shows this device's public key. Paste it on its own line in ~/.ssh/authorized_keys on the machine."
            ))]
        case (.mosh, .macOS):
            [
                SetupStep(
                    text: String(
                        localized: "tutorial.setup.mosh.why",
                        defaultValue: "Mosh keeps a session attached when you change networks or your device sleeps. With Homebrew:"
                    ),
                    command: "brew install mosh"
                ),
            ]
        case (.mosh, .linux):
            [
                SetupStep(
                    text: String(
                        localized: "tutorial.setup.mosh.why.linux",
                        defaultValue: "Mosh keeps a session attached when you change networks or your device sleeps. On Debian or Ubuntu:"
                    ),
                    command: "sudo apt install mosh"
                ),
                SetupStep(
                    text: String(localized: "tutorial.setup.mosh.fedora", defaultValue: "On Fedora:"),
                    command: "sudo dnf install mosh"
                ),
                SetupStep(
                    text: String(localized: "tutorial.setup.mosh.arch", defaultValue: "On Arch:"),
                    command: "sudo pacman -S mosh"
                ),
            ]
        case (.helper, _):
            [
                SetupStep(
                    text: String(localized: "tutorial.setup.helper.install", defaultValue: "Run this on the machine:"),
                    command: HelperProbe.installCommand(os: os.unameName)
                ),
                SetupStep(
                    text: String(localized: "tutorial.setup.helper.uninstall", defaultValue: "To remove it later:"),
                    command: "togo uninstall"
                ),
            ]
        }
    }
}

/// The standard setup as a list to tick off. Each item opens to its exact steps for the machine
/// kind picked at the top.
struct SetupChecklist: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let ticked: Set<SetupItem>
    let detected: Set<SetupItem>
    let onToggle: (SetupItem) -> Void
    @State private var os: MachineOS = .macOS
    /// The item whose steps are open, or closing.
    @State private var expanded: SetupItem?
    /// How far its steps are open: they reveal as the card grows, and conceal before it shrinks.
    @State private var steps: Disclosure

    init(
        ticked: Set<SetupItem>, detected: Set<SetupItem>, expanded: SetupItem? = nil, onToggle: @escaping (SetupItem) -> Void
    ) {
        self.ticked = ticked
        self.detected = detected
        self.onToggle = onToggle
        _expanded = State(initialValue: expanded)
        _steps = State(initialValue: Disclosure(open: expanded != nil))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Picker(String(localized: "tutorial.setup.os", defaultValue: "Machine"), selection: $os) {
                ForEach(MachineOS.allCases, id: \.self) { Text($0.name).tag($0) }
            }
            .pickerStyle(.segmented)
            VStack(spacing: 0) {
                ForEach(SetupItem.allCases, id: \.self) { item in
                    if item != SetupItem.allCases.first {
                        Divider().overlay(Color.togoLine).padding(.leading, 56)
                    }
                    SetupItemRow(
                        item: item, os: os, ticked: ticked.contains(item), detected: detected.contains(item),
                        steps: expanded == item ? steps : Disclosure(),
                        onToggle: { onToggle(item) },
                        onExpand: { expand(item) }
                    )
                }
            }
            .background(Color.togoGrouped, in: .rect(cornerRadius: 16))
            // Steps leaving never draw past the card as it shrinks.
            .clipShape(.rect(cornerRadius: 16))
        }
    }

    /// The open item closes the spec's way, content first; another item opens in one `resize`
    /// while the steps it replaces conceal.
    private func expand(_ item: SetupItem) {
        if expanded == item {
            Motion.toggle($steps, reduce: reduceMotion)
            return
        }
        withAnimation(Motion.animation(.resize, reduce: reduceMotion)) {
            expanded = item
            Motion.set($steps, open: true)
        }
    }
}

private struct SetupItemRow: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let item: SetupItem
    let os: MachineOS
    let ticked: Bool
    let detected: Bool
    /// Its steps: open, closing or closed.
    let steps: Disclosure
    let onToggle: () -> Void
    let onExpand: () -> Void

    private var expanded: Bool { steps.isOpen }

    var body: some View {
        let stacked = dynamicTypeSize.isAccessibilitySize
        VStack(alignment: .leading, spacing: 0) {
            // At accessibility sizes the tick sits above the title, so the title gets the full width.
            if stacked {
                VStack(alignment: .leading, spacing: 0) {
                    tick
                    expandButton
                }
                .padding(.horizontal, 12)
            } else {
                HStack(alignment: .center, spacing: 4) {
                    tick
                    expandButton
                }
                .padding(.leading, 6)
                .padding(.trailing, 16)
            }
            if steps.open {
                VStack(alignment: .leading, spacing: 14) {
                    ForEach(Array(item.steps(for: os).enumerated()), id: \.offset) { _, step in
                        SetupStepView(step: step)
                    }
                }
                .padding(.leading, stacked ? 16 : 56)
                .padding(.trailing, 16)
                .padding(.bottom, 16)
                .disclosed(steps)
            }
        }
    }

    private var tick: some View {
        Button(action: onToggle) {
            Image(systemName: ticked ? "checkmark.circle.fill" : "circle")
                .font(.title2)
                .foregroundStyle(ticked ? Color.togoSnow : Color.togoMute)
                .contentTransition(reduceMotion ? .identity : .symbolEffect(.replace))
                .frame(minWidth: 44, minHeight: 44)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(item.title)
        .accessibilityValue(ticked
            ? String(localized: "tutorial.setup.done", defaultValue: "Done")
            : String(localized: "tutorial.setup.notDone", defaultValue: "Not done"))
        .accessibilityAddTraits(.isToggle)
    }

    private var expandButton: some View {
        Button(action: onExpand) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(item.title)
                        .font(.body)
                        .foregroundStyle(Color.togoSnow)
                    if let detail {
                        Text(detail)
                            .font(.subheadline)
                            .foregroundStyle(Color.togoMute)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                Image(systemName: "chevron.down")
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(Color.togoMute)
                    .rotationEffect(.degrees(expanded ? 180 : 0))
                    .motion(.respond, value: expanded)
            }
            .padding(.vertical, 12)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityHint(String(localized: "tutorial.setup.showSteps", defaultValue: "Shows the steps"))
    }

    private var detail: String? {
        if detected {
            return String(localized: "tutorial.setup.detected", defaultValue: "Found on a machine you added")
        }
        return item.isOptional ? String(localized: "tutorial.setup.optional", defaultValue: "Optional") : nil
    }
}

/// One step's sentence, and its command with a Copy button that copies exactly that command.
struct SetupStepView: View {
    let step: SetupStep
    @State private var copied = false

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(step.text)
                .font(.subheadline)
                .foregroundStyle(Color.togoMute)
                .fixedSize(horizontal: false, vertical: true)
            if let command = step.command {
                CopyableText(text: command)
                Button {
                    UIPasteboard.general.string = command
                    copied = true
                } label: {
                    Label(
                        copied
                            ? String(localized: "tutorial.copied", defaultValue: "Copied")
                            : String(localized: "tutorial.copy", defaultValue: "Copy"),
                        systemImage: copied ? "checkmark" : "doc.on.doc"
                    )
                    .contentTransition(.identity)
                }
                .buttonStyle(.secondary(.fit))
                .accessibilityHint(Text(verbatim: command))
            }
        }
        .task(id: copied) {
            guard copied else { return }
            try? await Task.sleep(for: .seconds(2))
            copied = false
        }
    }
}

struct SetupPage: View {
    let model: TutorialModel
    var expanded: SetupItem?

    var body: some View {
        TutorialPageLayout(page: .setup, centered: false) {
            SetupChecklist(ticked: model.ticked, detected: model.detected, expanded: expanded, onToggle: model.toggle)
        }
    }
}
