import SwiftUI
import TogoKit

/// What one machine's helper lets this device change. The notification settings come first;
/// `extra` adds sections of its own below them, then the machine's agent integrations.
struct MachineHelperView<Extra: View>: View {
    let machine: HostID
    let name: String
    /// Whether the machine's helper advertised notification settings.
    let supportsNotify: Bool
    private let extra: Extra

    @Environment(\.machineTools) private var tools
    /// The demo machine has no helper to ask, so its page says so instead of failing to load.
    @Environment(\.demoExit) private var demoExit

    init(
        machine: HostID, name: String, supportsNotify: Bool, @ViewBuilder extra: () -> Extra
    ) {
        self.machine = machine
        self.name = name
        self.supportsNotify = supportsNotify
        self.extra = extra()
    }

    var body: some View {
        if let demoExit {
            DemoNotHereContent(exit: demoExit)
                .navigationTitle(name)
                .navigationBarTitleDisplayMode(.inline)
        } else {
            Loaded(machine: machine, name: name, supportsNotify: supportsNotify, tools: tools) { extra }
        }
    }

    /// Owns the model, which needs the tools from the environment.
    private struct Loaded<Content: View>: View {
        let machine: HostID
        let name: String
        let supportsNotify: Bool
        let content: Content
        @State private var model: MachineNotifyModel

        init(
            machine: HostID, name: String, supportsNotify: Bool, tools: any MachineTools,
            @ViewBuilder content: () -> Content
        ) {
            self.machine = machine
            self.name = name
            self.supportsNotify = supportsNotify
            self.content = content()
            _model = State(initialValue: MachineNotifyModel(host: machine, name: name, tools: tools, supported: supportsNotify))
        }

        var body: some View {
            List {
                HelperUpdateSection(machine: machine, name: name)
                MachineNotifySection(name: name, supported: supportsNotify, model: model)
                content
                MachineIntegrationsSection(machine: machine)
            }
            .groupedList()
            .navigationTitle(name)
            .navigationBarTitleDisplayMode(.inline)
            .task { await model.load() }
            .onDisappear { Task { await model.flush() } }
        }
    }
}

extension MachineHelperView where Extra == EmptyView {
    init(machine: HostID, name: String, supportsNotify: Bool) {
        self.init(machine: machine, name: name, supportsNotify: supportsNotify) { EmptyView() }
    }
}
