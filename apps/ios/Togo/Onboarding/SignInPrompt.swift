import SwiftUI
import TogoKit

/// What a machine asked for while signing in, in the server's own words.
struct SignInRequest: Hashable, Sendable {
    enum Kind: Hashable, Sendable {
        case password, code
    }

    struct Field: Hashable, Sendable {
        /// The server's prompt, such as "Verification code:". Shown as data, never as copy.
        var label: String
        var secret: Bool
    }

    var kind: Kind
    var machine: String
    /// The server's name and instruction for this round, if it sent any.
    var instruction: String
    var fields: [Field]
    /// Whether the round answers a plain password prompt rather than keyboard-interactive.
    var isPasswordPrompt: Bool

    /// Server text is capped and stripped of controls and direction overrides, so a hostile
    /// server can neither flood the sheet nor make its words read as something else.
    static let maxText = 500
    static let maxFields = 8

    static func clean(_ text: String) -> String {
        let scalars = text.unicodeScalars.filter { scalar in
            if scalar == "\n" { return true }
            switch scalar.properties.generalCategory {
            case .control, .format: return false
            default: return true
            }
        }
        let cleaned = String(String.UnicodeScalarView(scalars)).trimmingCharacters(in: .whitespacesAndNewlines)
        return cleaned.count > maxText ? String(cleaned.prefix(maxText)) + "\u{2026}" : cleaned
    }

    /// Nil for prompts the user cannot answer here, such as a key passphrase.
    init?(_ prompt: AuthPrompt, machine: String) {
        self.machine = machine
        switch prompt {
        case .password(let user, _):
            kind = .password
            instruction = ""
            fields = [Field(
                label: String(localized: "onboard.signIn.passwordFor", defaultValue: "Password for \(user)"), secret: true
            )]
            isPasswordPrompt = true
        case .keyboardInteractive(let name, let instruction, let prompts):
            guard !prompts.isEmpty, prompts.count <= Self.maxFields else { return nil }
            self.instruction = Self.clean([name, instruction]
                .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
                .filter { !$0.isEmpty }
                .joined(separator: "\n"))
            fields = prompts.map { Field(label: Self.clean($0.text), secret: !$0.echo) }
            let onlyPassword = prompts.count == 1 && prompts[0].text.localizedCaseInsensitiveContains("password")
            kind = onlyPassword ? .password : .code
            isPasswordPrompt = false
        case .passphrase:
            return nil
        }
    }

    init(kind: Kind, machine: String, instruction: String, fields: [Field]) {
        self.kind = kind
        self.machine = machine
        self.instruction = instruction
        self.fields = fields
        isPasswordPrompt = false
    }

    func answer(_ values: [String]) -> AuthAnswer {
        isPasswordPrompt ? .secret(values.first ?? "") : .responses(values)
    }

    var title: String {
        switch kind {
        case .code: String(localized: "onboard.signIn.code.title", defaultValue: "\(machine) is asking for a code")
        case .password: String(localized: "onboard.signIn.password.title", defaultValue: "\(machine) is asking for its password")
        }
    }
}

/// The answer form for a `SignInRequest`. What the user types goes to the server and is not
/// kept.
struct SignInPromptView: View {
    let request: SignInRequest
    /// Why the last answer did not work, if it did not.
    var error: String?
    let onSubmit: ([String]) -> Void

    @State private var values: [String]
    @FocusState private var focused: Int?

    init(request: SignInRequest, error: String? = nil, onSubmit: @escaping ([String]) -> Void) {
        self.request = request
        self.error = error
        self.onSubmit = onSubmit
        _values = State(initialValue: Array(repeating: "", count: request.fields.count))
    }

    var body: some View {
        OnboardingStep(
            title: request.title,
            message: String(
                localized: "onboard.signIn.message",
                defaultValue: "Togo passes your answer to \(request.machine) and doesn't keep it."
            )
        ) {
            if !request.instruction.isEmpty {
                Text(verbatim: request.instruction)
                    .font(.body)
                    .foregroundStyle(Color.togoSnow)
            }
            ForEach(request.fields.indices, id: \.self) { index in
                field(index)
            }
            if let error {
                InlineError(message: error)
            }
        } actions: {
            Button(String(localized: "onboard.signIn.continue", defaultValue: "Continue"), action: submit)
                .buttonStyle(.primary)
                .disabled(values.contains { $0.isEmpty })
        }
        .onAppear { focused = 0 }
    }

    private func field(_ index: Int) -> some View {
        let spec = request.fields[index]
        return VStack(alignment: .leading, spacing: 6) {
            Text(verbatim: spec.label)
                .font(.subheadline)
                .foregroundStyle(Color.togoMute)
            Group {
                if spec.secret {
                    SecureField(text: $values[index]) { EmptyView() }
                } else {
                    TextField(text: $values[index]) { EmptyView() }
                }
            }
            .accessibilityLabel(Text(verbatim: spec.label))
            .textContentType(request.kind == .code ? .oneTimeCode : .password)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            .font(.body.monospaced())
            .foregroundStyle(Color.togoSnow)
            .padding(.horizontal, 12)
            .frame(minHeight: 44)
            .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Color.togoLine))
            .focused($focused, equals: index)
            .submitLabel(index == request.fields.count - 1 ? .continue : .next)
            .onSubmit {
                if index + 1 < request.fields.count { focused = index + 1 } else { submit() }
            }
        }
    }

    private func submit() {
        guard !values.contains(where: \.isEmpty) else { return }
        onSubmit(values)
    }
}
