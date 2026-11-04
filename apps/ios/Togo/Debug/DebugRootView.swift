#if DEBUG
import SwiftUI
import TogoKit

struct DebugRootView: View {
    @State private var hosts = DebugComposition.shared.hosts
    @State private var editing: HostProfile?
    @State private var adding = false
    @State private var keyMessage: String?

    var body: some View {
        NavigationStack {
            List {
                Section("Hosts") {
                    ForEach(hosts.profiles) { profile in
                        NavigationLink(value: profile) {
                            VStack(alignment: .leading) {
                                Text(profile.name)
                                Text("\(profile.endpoint.user)@\(profile.endpoint.host)")
                                    .font(.caption).foregroundStyle(.secondary)
                            }
                        }
                        .swipeActions(edge: .leading) {
                            Button("Edit") { editing = profile }
                        }
                    }
                    .onDelete { hosts.delete(at: $0) }
                    Button("Add host") { adding = true }
                }
                Section("Keys") {
                    Button("Copy public key") { Task { await copyPublicKey() } }
                    if let keyMessage {
                        Text(keyMessage).font(.caption).foregroundStyle(.secondary)
                    }
                }
                Section("Tools") {
                    NavigationLink("Rendering bench") { DebugBenchView() }
                }
            }
            .navigationTitle("Debug")
            .navigationDestination(for: HostProfile.self) { DebugSessionsView(profile: $0) }
            .sheet(isPresented: $adding) {
                NavigationStack { DebugHostForm(existing: nil) { hosts.save($0) } }
            }
            .sheet(item: $editing) { profile in
                NavigationStack { DebugHostForm(existing: profile) { hosts.save($0) } }
            }
        }
    }

    private func copyPublicKey() async {
        do {
            let key = try await DebugComposition.shared.deviceKey()
            UIPasteboard.general.string = key.publicKeyLine
            keyMessage = "Copied. Add it to ~/.ssh/authorized_keys on the host. \(key.fingerprint)"
        } catch {
            keyMessage = error.localizedDescription
        }
    }
}
#endif
