#if DEBUG
import SwiftUI
import TogoKit

struct DebugHostForm: View {
    enum AuthChoice: String, CaseIterable, Identifiable {
        case deviceKey = "Device key"
        case clipboardKey = "Key from clipboard"
        case password = "Password"
        var id: Self { self }
    }

    let existing: HostProfile?
    let onSave: (HostProfile) -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var name = ""
    @State private var address = ""
    @State private var port = "22"
    @State private var user = ""
    @State private var auth: AuthChoice = .deviceKey
    @State private var passphrase = ""
    @State private var jump = ""
    @State private var transport: TransportPreference = .auto
    @State private var etAvailable = false
    @State private var moshServerPath = ""
    @State private var error: String?
    @State private var saving = false

    init(existing: HostProfile?, onSave: @escaping (HostProfile) -> Void) {
        self.existing = existing
        self.onSave = onSave
        guard let p = existing else { return }
        _name = State(initialValue: p.name)
        _address = State(initialValue: p.endpoint.host)
        _port = State(initialValue: String(p.endpoint.port))
        _user = State(initialValue: p.endpoint.user)
        _auth = State(initialValue: p.auth == .password ? .password : .deviceKey)
        _jump = State(initialValue: p.jumps.first.map { "\($0.user)@\($0.host):\($0.port)" } ?? "")
        _transport = State(initialValue: p.transport)
        _etAvailable = State(initialValue: p.etAvailable)
        _moshServerPath = State(initialValue: p.moshServerPath ?? "")
    }

    var body: some View {
        Form {
            Section("Host") {
                TextField("Name", text: $name)
                TextField("Address", text: $address, prompt: Text("Tailscale name"))
                    .textInputAutocapitalization(.never).autocorrectionDisabled()
                TextField("Port", text: $port).keyboardType(.numberPad)
                TextField("User", text: $user)
                    .textInputAutocapitalization(.never).autocorrectionDisabled()
            }
            Section("Sign in") {
                Picker("Method", selection: $auth) {
                    ForEach(AuthChoice.allCases) { Text($0.rawValue).tag($0) }
                }
                if auth == .clipboardKey {
                    SecureField("Key passphrase, if any", text: $passphrase)
                }
            }
            Section("Route") {
                TextField("Jump host", text: $jump, prompt: Text("user@host:22"))
                    .textInputAutocapitalization(.never).autocorrectionDisabled()
                Picker("Transport", selection: $transport) {
                    Text("Auto").tag(TransportPreference.auto)
                    Text("SSH").tag(TransportPreference.ssh)
                    Text("Mosh").tag(TransportPreference.mosh)
                    Text("ET").tag(TransportPreference.et)
                }
                Toggle("ET available", isOn: $etAvailable)
                TextField("mosh-server path", text: $moshServerPath, prompt: Text("Found on PATH"))
                    .textInputAutocapitalization(.never).autocorrectionDisabled()
            }
            if let error {
                Section { Text(error).foregroundStyle(.red) }
            }
        }
        .navigationTitle(existing == nil ? "New host" : "Edit host")
        .toolbar {
            ToolbarItem(placement: .cancellationAction) { Button("Cancel", systemImage: "xmark", role: .cancel) { dismiss() } }
            ToolbarItem(placement: .confirmationAction) {
                Button("Save", systemImage: "checkmark", role: .confirm) { Task { await save() } }.disabled(saving || address.isEmpty || user.isEmpty)
            }
        }
    }

    private func save() async {
        saving = true
        defer { saving = false }
        do {
            let profile = HostProfile(
                id: existing?.id ?? UUID(),
                name: name.isEmpty ? address : name,
                endpoint: HostEndpoint(host: address, port: Int(port) ?? 22, user: user),
                jumps: try parseJump(),
                auth: try await authRef(),
                transport: transport,
                moshServerPath: moshServerPath.isEmpty ? nil : moshServerPath,
                etAvailable: etAvailable
            )
            onSave(profile)
            dismiss()
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func authRef() async throws -> AuthMethodRef {
        switch auth {
        case .password:
            return .password
        case .deviceKey:
            if case .key(let id)? = existing?.auth { return .key(id: id) }
            return .key(id: try await DebugComposition.shared.deviceKey().id)
        case .clipboardKey:
            guard let text = UIPasteboard.general.string, text.contains("PRIVATE KEY") else {
                throw FormError("The clipboard has no OpenSSH private key.")
            }
            let key = try await DebugComposition.shared.env.keys.importKey(
                openSSH: text, passphrase: passphrase.isEmpty ? nil : passphrase, label: "Imported key"
            )
            UIPasteboard.general.string = ""
            return .key(id: key.id)
        }
    }

    private func parseJump() throws -> [HostEndpoint] {
        let text = jump.trimmingCharacters(in: .whitespaces)
        guard !text.isEmpty else { return [] }
        let parts = text.split(separator: "@", maxSplits: 1)
        guard parts.count == 2 else { throw FormError("Write the jump host as user@host or user@host:port.") }
        let hostPort = parts[1].split(separator: ":", maxSplits: 1)
        let port = hostPort.count == 2 ? Int(hostPort[1]) : 22
        guard let port else { throw FormError("The jump host port is not a number.") }
        return [HostEndpoint(host: String(hostPort[0]), port: port, user: String(parts[0]))]
    }

    private struct FormError: LocalizedError {
        let errorDescription: String?
        init(_ text: String) { errorDescription = text }
    }
}
#endif
