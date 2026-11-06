import Foundation
import TogoKit

/// How the user signs in to a machine they are adding.
enum DraftAuth: Hashable, Sendable, CaseIterable {
    case deviceKey, importedKey, password

    var label: String {
        switch self {
        case .deviceKey: String(localized: "onboard.auth.deviceKey", defaultValue: "This phone's key")
        case .importedKey: String(localized: "onboard.auth.importedKey", defaultValue: "Import a key")
        case .password: String(localized: "onboard.auth.password", defaultValue: "Password")
        }
    }
}

/// A key imported from a file, already in the keychain.
struct ImportedKey: Hashable, Sendable {
    var id: String
    var label: String
}

/// The machine form's fields as typed. Validation reads them as strings so a half-typed
/// port shows an inline error instead of silently becoming 22.
struct MachineDraft: Hashable, Sendable {
    enum Field: Hashable, Sendable, CaseIterable {
        case name, address, port, user, key, jump
    }

    var name = ""
    var address = ""
    var port = "22"
    var user = ""
    var symbol: HostSymbol = .laptop
    var auth: DraftAuth = .deviceKey
    var importedKey: ImportedKey?
    var passphrase = ""
    var password = ""
    /// Keeps the password in this device's keychain so Togo can sign in on its own.
    var savePassword = true
    var jump = ""
    /// The wake target the machine reported; kept as is when the machine is edited.
    var wake: WakeTarget?

    init() {}

    init(discovered host: DiscoveredHost) {
        name = Self.friendlyName(host.name)
        address = host.host
        port = String(host.port)
        symbol = host.name.localizedStandardContains("MacBook") ? .laptop : .desktop
    }

    var trimmedName: String { name.trimmingCharacters(in: .whitespacesAndNewlines) }

    /// One message per field that is wrong, written as what to do next.
    var errors: [Field: String] {
        var errors: [Field: String] = [:]
        if trimmedName.isEmpty {
            errors[.name] = String(localized: "onboard.form.error.name", defaultValue: "Give this machine a name.")
        }
        let host = address.trimmingCharacters(in: .whitespaces)
        if host.isEmpty {
            errors[.address] = String(localized: "onboard.form.error.addressEmpty", defaultValue: "Enter a host name or IP address.")
        } else if host.hasPrefix("-") {
            errors[.address] = String(
                localized: "onboard.form.error.addressDash", defaultValue: "A host name can't start with a dash."
            )
        } else if host.contains(where: { $0.isWhitespace || $0 == "@" || $0 == "/" }) {
            errors[.address] = String(
                localized: "onboard.form.error.addressShape", defaultValue: "Enter only the host name, without a user or path."
            )
        }
        if Self.port(port) == nil {
            errors[.port] = String(localized: "onboard.form.error.port", defaultValue: "Use a port from 1 to 65535.")
        }
        let user = user.trimmingCharacters(in: .whitespaces)
        if user.isEmpty {
            errors[.user] = String(localized: "onboard.form.error.userEmpty", defaultValue: "Enter the user name you sign in with.")
        } else if user.hasPrefix("-") {
            errors[.user] = String(localized: "onboard.form.error.userDash", defaultValue: "A user name can't start with a dash.")
        } else if user.contains(where: \.isWhitespace) {
            errors[.user] = String(localized: "onboard.form.error.userSpaces", defaultValue: "User names can't contain spaces.")
        }
        if auth == .importedKey, importedKey == nil {
            errors[.key] = String(localized: "onboard.form.error.key", defaultValue: "Choose a key file to import.")
        }
        if !jump.trimmingCharacters(in: .whitespaces).isEmpty, Self.endpoint(jump) == nil {
            errors[.jump] = String(
                localized: "onboard.form.error.jump", defaultValue: "Write the jump host as user@host or user@host:port."
            )
        }
        return errors
    }

    var isValid: Bool { errors.isEmpty }

    /// The profile to connect with, or nil while the form has errors. `deviceKeyID` is the
    /// phone's own key, used when the user picked it.
    func profile(id: UUID, deviceKeyID: String?) -> HostProfile? {
        guard isValid, let port = Self.port(port) else { return nil }
        let auth: AuthMethodRef
        switch self.auth {
        case .deviceKey:
            guard let deviceKeyID else { return nil }
            auth = .key(id: deviceKeyID)
        case .importedKey:
            guard let importedKey else { return nil }
            auth = .key(id: importedKey.id)
        case .password:
            auth = .password
        }
        let jumps = jump.trimmingCharacters(in: .whitespaces).isEmpty ? [] : Self.endpoint(jump).map { [$0] } ?? []
        return HostProfile(
            id: id, name: trimmedName,
            endpoint: HostEndpoint(
                host: address.trimmingCharacters(in: .whitespaces), port: port, user: user.trimmingCharacters(in: .whitespaces)
            ),
            jumps: jumps, auth: auth
        )
    }

    private static func port(_ text: String) -> Int? {
        guard let value = Int(text.trimmingCharacters(in: .whitespaces)), (1...65535).contains(value) else { return nil }
        return value
    }

    /// Parses `user@host`, `user@host:port` or `user@[v6 address]:port`.
    static func endpoint(_ text: String) -> HostEndpoint? {
        let text = text.trimmingCharacters(in: .whitespaces)
        guard let at = text.firstIndex(of: "@") else { return nil }
        let user = String(text[..<at])
        var host = String(text[text.index(after: at)...])
        var port = 22
        if host.hasPrefix("["), let close = host.firstIndex(of: "]") {
            let rest = host[host.index(after: close)...]
            if rest.hasPrefix(":") {
                guard let parsed = Self.port(String(rest.dropFirst())) else { return nil }
                port = parsed
            } else if !rest.isEmpty {
                return nil
            }
            host = String(host[host.index(after: host.startIndex)..<close])
        } else if host.filter({ $0 == ":" }).count == 1, let colon = host.firstIndex(of: ":") {
            guard let parsed = Self.port(String(host[host.index(after: colon)...])) else { return nil }
            port = parsed
            host = String(host[..<colon])
        }
        guard !user.isEmpty, !host.isEmpty, !user.hasPrefix("-"), !host.hasPrefix("-"),
              !(user + host).contains(where: \.isWhitespace) else { return nil }
        return HostEndpoint(host: host, port: port, user: user)
    }

    /// Bonjour names a Mac "Sam's MacBook Pro"; the possessive is noise in a list of the
    /// user's own machines.
    private static func friendlyName(_ name: String) -> String {
        for marker in ["'s ", "\u{2019}s "] {
            if let range = name.range(of: marker) {
                return String(name[range.upperBound...])
            }
        }
        return name
    }
}
