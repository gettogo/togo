import SwiftUI
import TogoKit

/// The helper's release row on a machine's page. Placement: the first section of the page's
/// grouped list on every layout (the page is pushed in the Settings column on iPad and the
/// unfolded Duo, and fills the screen on iPhone and the folded Duo). The text and the system
/// button never share a line at accessibility sizes, and the button never shrinks.
struct HelperUpdateSection: View {
    let machine: HostID
    let name: String

    @Environment(\.helperUpdates) private var updates
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize

    var body: some View {
        if let updates { content(updates) }
    }

    @ViewBuilder private func content(_ updates: HelperUpdates) -> some View {
        let state = updates.state(machine)
        let phase = updates.phase(machine)
        let latest = updates.available(machine, current: state?.installed)
        if updates.isHomebrew(machine) {
            Section { HomebrewRow() }.groupedRows()
        } else if latest != nil || phase != .idle {
            Section {
                row(updates, installed: state?.installed, latest: latest, phase: phase)
            }
            .groupedRows()
        }
    }

    @ViewBuilder private func row(_ updates: HelperUpdates, installed: String?, latest: String?, phase: HelperUpdates.Phase) -> some View {
        let layout = dynamicTypeSize.isAccessibilitySize ? AnyLayout(VStackLayout(alignment: .leading, spacing: 12))
            : AnyLayout(HStackLayout(alignment: .center, spacing: 12))
        layout {
            VStack(alignment: .leading, spacing: 2) {
                Text(title(installed: installed, phase: phase))
                    .foregroundStyle(Color.togoSnow)
                if let detail = detail(latest: latest, phase: phase) {
                    Text(detail)
                        .font(.subheadline)
                        .foregroundStyle(Color.togoMute)
                }
                if case .failed(let message) = phase {
                    Text(message)
                        .font(.subheadline)
                        .foregroundStyle(Color.primary)
                }
            }
            .fixedSize(horizontal: false, vertical: true)
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityElement(children: .combine)
            if case .updated = phase {
                EmptyView()
            } else {
                button(updates, phase: phase)
            }
        }
        .padding(.vertical, 4)
        .frame(minHeight: 44)
    }

    @ViewBuilder private func button(_ updates: HelperUpdates, phase: HelperUpdates.Phase) -> some View {
        if phase == .updating {
            ProgressView()
                .accessibilityLabel(HelperUpdateCopy.updating)
        } else {
            Button(HelperUpdateCopy.update) {
                Task { await updates.update(machine) }
            }
            .buttonStyle(.bordered)
            .fixedSize()
            .accessibilityLabel(settingsText("helperUpdate.update.for", "Update helper on \(name)"))
        }
    }

    private func title(installed: String?, phase: HelperUpdates.Phase) -> String {
        switch phase {
        case .updating: HelperUpdateCopy.updating
        case .updated(let version): HelperUpdateCopy.updated(version.map(HelperUpdate.display))
        default: installed.map { HelperUpdateCopy.installed(HelperUpdate.display($0)) } ?? HelperUpdateCopy.title
        }
    }

    private func detail(latest: String?, phase: HelperUpdates.Phase) -> String? {
        switch phase {
        case .updating, .updated: nil
        default: latest.map { HelperUpdateCopy.available(HelperUpdate.display($0)) }
        }
    }
}

/// A Homebrew install updates through brew, so the app runs nothing and offers the command.
private struct HomebrewRow: View {
    @State private var copied = false

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(HelperUpdateCopy.homebrew)
                .foregroundStyle(Color.togoSnow)
                .fixedSize(horizontal: false, vertical: true)
            Text(HelperUpdate.brewCommand)
                .font(.body.monospaced())
                .textSelection(.enabled)
                .foregroundStyle(Color.togoSnow)
                .fixedSize(horizontal: false, vertical: true)
            Button(copied ? HelperUpdateCopy.copied : HelperUpdateCopy.copy, systemImage: copied ? "checkmark" : "doc.on.doc") {
                UIPasteboard.general.string = HelperUpdate.brewCommand
                copied = true
            }
            .labelStyle(.titleAndIcon)
            .buttonStyle(.bordered)
            .fixedSize()
            .accessibilityLabel(settingsText("helperUpdate.copy.label", "Copy brew upgrade togo"))
        }
        .padding(.vertical, 4)
        .frame(minHeight: 44, alignment: .leading)
    }
}
