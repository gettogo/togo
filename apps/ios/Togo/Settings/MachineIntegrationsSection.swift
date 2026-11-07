import SwiftUI
import TogoKit

/// A machine's agent integrations as sections of a list. It owns its model, which needs the
/// tools from the environment.
struct MachineIntegrationsSection: View {
    let machine: HostID

    @Environment(\.machineTools) private var tools

    var body: some View {
        Loaded(machine: machine, tools: tools)
    }

    private struct Loaded: View {
        @State private var model: IntegrationsModel
        let machine: HostID

        init(machine: HostID, tools: any MachineTools) {
            self.machine = machine
            _model = State(initialValue: IntegrationsModel(host: machine, tools: tools))
        }

        var body: some View {
            IntegrationsRows(model: model)
            AgentSignInSection(machine: machine)
        }
    }
}

/// A page of its own for one machine's integrations, reached from Settings › Agents.
struct MachineIntegrationsPage: View {
    let machine: HostID
    let name: String

    var body: some View {
        List {
            MachineIntegrationsSection(machine: machine)
        }
        .groupedList()
        .navigationTitle(name)
        .navigationBarTitleDisplayMode(.inline)
    }
}

/// The rows over a model the caller supplies: one per installed harness that takes setup,
/// Set up all when more than one needs it, and the harnesses that need nothing listed once.
struct IntegrationsRows: View {
    let model: IntegrationsModel
    @State private var removing: IntegrationReport?

    var body: some View {
        Section {
            switch model.phase {
            case .loading:
                ProgressView()
                    .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                    .accessibilityLabel(IntegrationsCopy.title)
            case .failed:
                Button(IntegrationsCopy.retry) { Task { await model.load() } }
                    .frame(minHeight: 44, alignment: .leading)
            case .unsupported:
                Text(IntegrationsCopy.unsupported)
                    .foregroundStyle(Color.togoMute)
                    .frame(minHeight: 44, alignment: .leading)
            case .loaded:
                ForEach(model.setupRows) { report in
                    IntegrationRow(report: report, busy: model.busy.contains(report.id), failure: model.failures[report.id]) {
                        Task { await model.setUp(report.id) }
                    }
                    .contextMenu {
                        if report.hooks == "installed" || report.hooks == "outdated" {
                            Button(IntegrationsCopy.remove, systemImage: "arrow.uturn.backward", role: .destructive) {
                                removing = report
                            }
                        }
                    }
                }
                if model.showsSetUpAll {
                    Button {
                        Task { await model.setUpAll() }
                    } label: {
                        Label(IntegrationsCopy.setUpAll, systemImage: "wand.and.stars")
                    }
                    .frame(minHeight: 44)
                    .disabled(!model.busy.isEmpty)
                }
            }
        } header: {
            // The header is always there, so the read hangs from it.
            Text(IntegrationsCopy.title)
                .font(.headline)
                .foregroundStyle(Color.togoSnow)
                .textCase(nil)
                .task { await model.load() }
        } footer: {
            if model.phase == .loaded, !model.setupRows.isEmpty {
                Text(IntegrationsCopy.footer)
            }
        }
        .groupedRows()
        .coordinated("settings.removeIntegration", .dialog, isPresented: Binding { removing != nil } set: { if !$0 { removing = nil } }) { view, shown in
            view.confirmationDialog(
                IntegrationsCopy.removeTitle(removing?.name ?? ""),
                isPresented: shown,
                titleVisibility: .visible,
                presenting: removing
            ) { report in
                Button(IntegrationsCopy.remove, role: .destructive) {
                    removing = nil
                    Task { await model.uninstall(report.id) }
                }
            } message: { _ in
                Text(IntegrationsCopy.removeMessage)
            }
        }
        // Loading → rows, a row's failure line and Set up all arrive on `resize`.
        .resizesOnRows([
            "\(model.phase)", model.setupRows.map(\.id).joined(separator: ","), "\(model.showsSetUpAll)",
            model.failures.keys.sorted().joined(separator: ","),
        ])
        if model.phase == .loaded, !model.notNeededRows.isEmpty {
            Section {
                ForEach(model.notNeededRows) { report in
                    LabeledContent {
                        Text(IntegrationsCopy.word(report.state)).foregroundStyle(Color.togoMute)
                    } label: {
                        Label {
                            Text(verbatim: report.name)
                        } icon: {
                            AgentGlyph(agent: report.id).foregroundStyle(Color.togoMute)
                        }
                    }
                    .frame(minHeight: 44)
                    .accessibilityElement(children: .combine)
                }
            } header: {
                Text(IntegrationsCopy.noSetupTitle)
            }
            .groupedRows()
        }
    }
}

/// One harness: its mark and name, the state word or the button that changes it, and a
/// failure line in place when the last change didn't take.
private struct IntegrationRow: View {
    let report: IntegrationReport
    let busy: Bool
    let failure: String?
    let setUp: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 12) {
                AgentGlyph(agent: report.id)
                    .font(.title3)
                    .frame(width: 28, alignment: .leading)
                VStack(alignment: .leading, spacing: 2) {
                    Text(verbatim: report.name)
                        .foregroundStyle(Color.togoSnow)
                    if let detail = IntegrationsCopy.detail(report) {
                        Text(detail)
                            .font(.subheadline)
                            .foregroundStyle(Color.togoMute)
                    }
                }
                Spacer(minLength: 8)
                trailing
            }
            if let failure {
                Label(failure, systemImage: "exclamationmark.circle")
                    .font(.footnote)
                    .foregroundStyle(Color.togoSnow)
            }
        }
        .frame(minHeight: 44)
        .accessibilityElement(children: .contain)
    }

    @ViewBuilder private var trailing: some View {
        if busy {
            ProgressView()
                .accessibilityLabel(IntegrationsCopy.setUp)
        } else if IntegrationsModel.needsAction(report.state) {
            HStack(spacing: 8) {
                // Attention in full ink, not the accent: the accent only ever means an agent
                // needs you. Ready rows read in mute.
                Text(IntegrationsCopy.word(report.state))
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(Color.togoSnow)
                Button(report.state == .outdated ? IntegrationsCopy.update : IntegrationsCopy.setUp, action: setUp)
                    .buttonStyle(.bordered)
                    .accessibilityLabel(Text(verbatim: "\(report.state == .outdated ? IntegrationsCopy.update : IntegrationsCopy.setUp) \(report.name)"))
            }
        } else {
            Text(IntegrationsCopy.word(report.state))
                .font(.subheadline)
                .foregroundStyle(Color.togoMute)
        }
    }
}
