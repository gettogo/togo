import SnapshotTesting
import SwiftUI
import Testing
import TogoKit
@testable import Togo

@MainActor
struct ImportHostsSnapshotTests {
    private static let config = """
    Host build
      HostName build.example.com
      User sam
      Port 2222
      ProxyJump bastion
    Host bastion
      HostName bastion.example.com
      User jump
    Host studio
      HostName studio.local
      User sam
    Host noUser
      HostName nouser.example.com
    Host lost
      HostName lost.example.com
      User sam
      ProxyJump ghost
    IdentityFile ~/.ssh/id_ed25519
    Match host x
    """

    private func model(text: String = "") -> ImportHostsModel {
        let id = UUID()
        let studio = HostRecord(
            id: id,
            profile: HostProfile(id: id, name: "Studio", endpoint: HostEndpoint(host: "studio.local", user: "sam"), auth: .password),
            symbol: "desktop"
        )
        let model = ImportHostsModel(existing: { [studio] }, deviceKeyID: { "key" }, save: { _ in })
        model.text = text
        return model
    }

    private func screen(_ model: ImportHostsModel) -> some View {
        NavigationStack { ImportHostsView(model: model) }.togoControls()
    }

    @Test func empty() {
        assertScreen(named: "import-hosts-empty") { screen(model()) }
    }

    @Test func tooLarge() {
        let model = model(text: String(repeating: "#", count: SSHConfigImport.maxBytes + 1))
        model.review()
        assertScreen(named: "import-hosts-too-large") { screen(model) }
    }

    @Test func preview() {
        let model = model(text: Self.config)
        model.review()
        assertScreen(named: "import-hosts-preview") { screen(model) }
    }

    @Test func machinesPageWithImport() {
        let values = SettingsValues.sample
        assertScreen(named: "machines-with-import") {
            NavigationStack {
                SettingsView(
                    values: values,
                    actions: SettingsActions(update: { _ in }, machineImport: MachineImport(
                        make: { self.model() }, openInForm: { _ in }, finished: { _ in }
                    ))
                ).page(.machines)
            }
            .togoControls()
        }
    }
}
