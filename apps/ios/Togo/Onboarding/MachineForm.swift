import SwiftUI
import TogoKit
import UniformTypeIdentifiers

/// Name, address, sign-in and an optional jump host for one machine. Mistakes show under
/// their field once the user tries to connect; nothing here raises an alert.
struct MachineForm: View {
    @Binding var draft: MachineDraft
    let showsErrors: Bool
    let importError: String?
    let onImportKey: (_ text: String, _ label: String) -> Void
    let onConnect: () -> Void

    @FocusState private var focus: MachineDraft.Field?
    @State private var choosingKeyFile = false
    @State private var jumpShown = false

    var body: some View {
        Form {
            Section {
                field(.name, String(localized: "onboard.form.name", defaultValue: "Name"), text: $draft.name, prompt: String(localized: "onboard.form.namePrompt", defaultValue: "MacBook, Studio, Build box"))
                    .textInputAutocapitalization(.words)
                // A long user@host must stay readable, so the address wraps instead of scrolling.
                field(
                    .address, String(localized: "onboard.form.address", defaultValue: "Address"), text: $draft.address,
                    prompt: "macbook.local", axis: .vertical
                )
                .keyboardType(.asciiCapable)
                if let addressNote = draft.addressNote {
                    note(addressNote)
                }
                field(.port, String(localized: "onboard.form.port", defaultValue: "Port"), text: $draft.port, prompt: "22")
                    .keyboardType(.numberPad)
                field(.user, String(localized: "onboard.form.user", defaultValue: "User"), text: $draft.user, prompt: String(localized: "onboard.form.userPrompt", defaultValue: "Your user name on the machine"))
            }
            .groupedRows()

            if draft.machineID != nil {
                MachineRoutesSection(draft: $draft)
            }

            Section {
                SymbolPicker(selection: $draft.symbol)
            } header: {
                SectionHeading(String(localized: "onboard.form.symbol", defaultValue: "Symbol"))
            }
            .groupedRows()

            Section {
                Picker(selection: $draft.auth) {
                    ForEach(DraftAuth.allCases, id: \.self) { auth in
                        Text(auth.label).tag(auth)
                    }
                } label: {
                    EmptyView()
                }
                .pickerStyle(.inline)
                .labelsHidden()
                authDetail
            } header: {
                SectionHeading(String(localized: "onboard.form.signIn", defaultValue: "Sign in with"))
            } footer: {
                if draft.auth == .deviceKey {
                    note(String(
                        localized: "onboard.form.deviceKeyNote",
                        defaultValue: "The key is made on this device and never leaves it. You add its public half to the machine."
                    ))
                } else if draft.auth == .password, draft.savePassword {
                    note(String(
                        localized: "onboard.form.savePasswordNote",
                        defaultValue: "Kept in this \(DeviceName.model)'s keychain behind \(BiometryName.current), never synced. Without it, Togo asks each time it signs in."
                    ))
                }
            }
            .groupedRows()

            // The machine's helper keeps the address current, so there is nothing to edit here.
            if WakeFeature.isEnabled, let wake = draft.wake {
                Section {
                    labelled(WakeCopy.formLabel) {
                        Text(verbatim: wake.mac)
                            .font(.system(.body, design: .monospaced))
                            .textSelection(.enabled)
                    }
                }
                .groupedRows()
            }

            Section {
                DisclosureGroup(isExpanded: $jumpShown) {
                    field(
                        .jump, String(localized: "onboard.form.jumpField", defaultValue: "Jump host"), text: $draft.jump,
                        prompt: "sam@bastion.example.com"
                    )
                    .keyboardType(.asciiCapable)
                } label: {
                    Text(String(localized: "onboard.form.jump", defaultValue: "Connect through a jump host"))
                        .foregroundStyle(Color.togoSnow)
                }
            }
            .groupedRows()
        }
        .groupedList()
        .navigationTitle(draft.trimmedName.isEmpty
            ? String(localized: "onboard.form.title", defaultValue: "New machine") : draft.trimmedName)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            // The number pad has no Return key, so the chain would stop at Port on a phone.
            ToolbarItemGroup(placement: .keyboard) {
                if focus == .port {
                    Spacer()
                    Button(String(localized: "onboard.form.next", defaultValue: "Next")) { advance(from: .port) }
                }
            }
            ToolbarItem(placement: .confirmationAction) {
                Button(String(localized: "onboard.form.connect", defaultValue: "Connect"), systemImage: "checkmark", role: .confirm, action: onConnect)
            }
        }
        .fileImporter(isPresented: $choosingKeyFile, allowedContentTypes: [.data, .plainText]) { result in
            guard case .success(let url) = result, let text = Self.readKey(url) else { return }
            onImportKey(text, url.lastPathComponent)
        }
        .onAppear {
            jumpShown = !draft.jump.isEmpty
            if draft.trimmedName.isEmpty { focus = .name } else if draft.user.isEmpty { focus = .user }
        }
    }

    @ViewBuilder private var authDetail: some View {
        switch draft.auth {
        case .deviceKey:
            EmptyView()
        case .importedKey:
            VStack(alignment: .leading, spacing: 8) {
                Button {
                    choosingKeyFile = true
                } label: {
                    Label(
                        draft.importedKey?.label ?? String(localized: "onboard.form.chooseKey", defaultValue: "Choose a key file"),
                        systemImage: draft.importedKey == nil ? "doc.badge.plus" : "key"
                    )
                    .foregroundStyle(Color.togoSnow)
                    .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                    .contentShape(.rect)
                }
                if let error = importError ?? visibleError(.key) { FieldError(text: error) }
            }
            labelled(String(localized: "onboard.form.passphrase", defaultValue: "Passphrase, if the key has one")) {
                SecureField("", text: $draft.passphrase)
                    .keptSecret()
                    .focused($focus, equals: .secret)
                    .submitLabel(.go)
                    .onSubmit { advance(from: .secret) }
            }
        case .password:
            labelled(String(localized: "onboard.form.passwordField", defaultValue: "Password")) {
                SecureField("", text: $draft.password)
                    .keptSecret()
                    .focused($focus, equals: .secret)
                    .submitLabel(.go)
                    .onSubmit { advance(from: .secret) }
            }
            Toggle(isOn: $draft.savePassword) {
                Text(String(localized: "onboard.form.savePassword", defaultValue: "Save password on this \(DeviceName.model)"))
                    .foregroundStyle(Color.togoSnow)
            }
            .frame(minHeight: 44)
        }
    }

    private func field(
        _ field: MachineDraft.Field, _ label: String, text: Binding<String>, prompt: String, axis: Axis = .horizontal
    ) -> some View {
        labelled(label, error: visibleError(field)) {
            TextField(label, text: text, prompt: Text(verbatim: prompt).foregroundStyle(Color.togoMute), axis: axis)
                .labelsHidden()
                .lineLimit(axis == .vertical ? 1...4 : 1...1)
                // Return in a wrapping field types a newline; treat it as the submit it is everywhere else.
                .onChange(of: text.wrappedValue) { _, value in
                    guard axis == .vertical, value.contains(where: \.isNewline) else { return }
                    text.wrappedValue = value.filter { !$0.isNewline }
                    advance(from: field)
                }
                .focused($focus, equals: field)
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
                .submitLabel(draft.field(after: field) == nil ? .go : .next)
                .onSubmit { advance(from: field) }
        }
    }

    private func labelled(_ label: String, error: String? = nil, @ViewBuilder input: () -> some View) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(label)
                .font(.subheadline)
                .foregroundStyle(Color.togoMute)
            input()
                .font(.body)
                .foregroundStyle(Color.togoSnow)
                .frame(minHeight: 32)
            if let error { FieldError(text: error) }
        }
        .padding(.vertical, 4)
        // The separator would otherwise start at the error's text, past its symbol.
        .alignmentGuide(.listRowSeparatorLeading) { $0[.leading] }
    }

    private func note(_ text: String) -> some View {
        Text(text)
            .font(.footnote)
            .foregroundStyle(Color.togoMute)
    }

    private func visibleError(_ field: MachineDraft.Field) -> String? {
        showsErrors ? draft.errors[field] : nil
    }

    private func advance(from field: MachineDraft.Field) {
        if let next = draft.field(after: field) {
            focus = next
        } else {
            focus = nil
            onConnect()
        }
    }

    private static func readKey(_ url: URL) -> String? {
        let scoped = url.startAccessingSecurityScopedResource()
        defer { if scoped { url.stopAccessingSecurityScopedResource() } }
        guard let data = try? Data(contentsOf: url), data.count < 64 * 1024 else { return nil }
        return String(data: data, encoding: .utf8)
    }
}

private struct FieldError: View {
    let text: String

    var body: some View {
        Label {
            Text(text)
        } icon: {
            Image(systemName: "exclamationmark.circle")
        }
        .font(.footnote)
        .foregroundStyle(Color.togoSnow)
        .accessibilityLabel(String(localized: "onboard.form.errorPrefix", defaultValue: "Error: \(text)"))
    }
}

/// Six symbols for telling machines apart at a glance.
private struct SymbolPicker: View {
    @Binding var selection: HostSymbol

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 8) { buttons }
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 52), spacing: 8)], spacing: 8) { buttons }
        }
        .padding(.vertical, 4)
    }

    private var buttons: some View {
        ForEach(HostSymbol.allCases, id: \.self) { symbol in
            let selected = symbol == selection
            Button {
                selection = symbol
            } label: {
                // A fixed size keeps all six on one row at every text size; the names are spoken.
                Image(systemName: symbol.systemImage)
                    .font(.system(size: 19))
                    .foregroundStyle(selected ? Color.togoInk : Color.togoSnow)
                    .frame(width: 44, height: 44)
                    .background(selected ? Color.togoSnow : Color.clear, in: .circle)
                    .overlay(Circle().strokeBorder(selected ? Color.clear : Color.togoLine))
                    .contentShape(.circle)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(symbol.spokenName)
            .accessibilityAddTraits(selected ? .isSelected : [])
            .frame(maxWidth: .infinity)
        }
    }
}

extension HostSymbol {
    var spokenName: String {
        switch self {
        case .laptop: String(localized: "onboard.symbol.laptop", defaultValue: "Laptop")
        case .desktop: String(localized: "onboard.symbol.desktop", defaultValue: "Desktop")
        case .server: String(localized: "onboard.symbol.server", defaultValue: "Server")
        case .mini: String(localized: "onboard.symbol.mini", defaultValue: "Mac mini")
        case .cloud: String(localized: "onboard.symbol.cloud", defaultValue: "Cloud machine")
        case .raspberry: String(localized: "onboard.symbol.board", defaultValue: "Small board")
        }
    }
}
