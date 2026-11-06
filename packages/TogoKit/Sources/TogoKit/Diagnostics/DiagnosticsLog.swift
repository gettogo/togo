import CryptoKit
import Foundation

/// A bounded log of typed events kept on this device as JSON lines. Nothing here is sent
/// anywhere; the person exports it by hand.
public actor DiagnosticsLog {
    public static let capacity = 2_000
    public static let maxFileBytes = 1_000_000

    private struct Entry {
        var event: DiagnosticEvent
        var line: Data
    }

    private let fileURL: URL
    private let salt: Data
    private var entries: [Entry] = []
    private var fileBytes = 0
    private var fileLines = 0
    private var known: [String] = []

    /// Events and the salt live in `directory`, which is excluded from backups.
    public init(directory: URL, salt: Data) {
        self.salt = salt
        fileURL = directory.appendingPathComponent("diagnostics.jsonl")
        Self.prepare(directory)
        let loaded = Self.load(fileURL)
        let kept = Array(loaded.suffix(Self.capacity))
        let bytes = loaded.reduce(0) { $0 + $1.line.count }
        entries = kept
        if loaded.count > kept.count || bytes > Self.maxFileBytes {
            fileBytes = Self.write(kept, to: fileURL)
            fileLines = kept.count
        } else {
            fileBytes = bytes
            fileLines = loaded.count
        }
    }

    /// The salt kept in `directory`, made from 32 random bytes the first time.
    public static func salt(in directory: URL) -> Data {
        prepare(directory)
        let url = directory.appendingPathComponent("diagnostics.salt")
        if let existing = try? Data(contentsOf: url), existing.count == 32 { return existing }
        let fresh = Data((0..<32).map { _ in UInt8.random(in: .min ... .max) })
        try? fresh.write(to: url, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
        exclude(url)
        return fresh
    }

    public func record(_ event: DiagnosticEvent) {
        guard let clean = event.sanitized(known: known), let line = Self.encode(clean) else { return }
        entries.append(Entry(event: clean, line: line))
        append(line)
        let over = entries.count - Self.capacity
        if over > 0 { entries.removeFirst(over) }
        if fileBytes > Self.maxFileBytes {
            // Drop down to three quarters so the next events do not rewrite the file again.
            var total = entries.reduce(0) { $0 + $1.line.count }
            var drop = 0
            while total > Self.maxFileBytes * 3 / 4, drop < entries.count - 1 {
                total -= entries[drop].line.count
                drop += 1
            }
            entries.removeFirst(drop)
            rewrite()
        } else if fileLines > Self.capacity + Self.capacity / 4 {
            rewrite()
        }
    }

    /// Names the person's machines and sessions carry, so they are removed from anything
    /// recorded from now on. Kept in memory only.
    public func learn(_ names: [String]) {
        let merged = Array(Set(known + names.filter { $0.count >= 3 })).sorted()
        guard merged != known else { return }
        known = merged
        // Events recorded before these names were known are cleaned too.
        entries = entries.compactMap { entry in
            guard let clean = entry.event.sanitized(known: merged), let line = Self.encode(clean) else { return nil }
            return Entry(event: clean, line: line)
        }
        rewrite()
    }

    public func events() -> [DiagnosticEvent] {
        entries.compactMap { $0.event.sanitized(known: known) }
    }

    public func clear() {
        entries = []
        rewrite()
    }

    /// The same host always gets the same token on this device, and under another salt a
    /// different one.
    public nonisolated func token(for host: UUID) -> MachineToken {
        let key = SymmetricKey(data: salt)
        let bytes = withUnsafeBytes(of: host.uuid) { Data($0) }
        let mac = HMAC<SHA256>.authenticationCode(for: bytes, using: key)
        let hex = mac.prefix(4).map { String(format: "%02x", $0) }.joined()
        return MachineToken(value: "m-\(hex)")
    }

    // MARK: Files

    private func append(_ line: Data) {
        let manager = FileManager.default
        if !manager.fileExists(atPath: fileURL.path) {
            manager.createFile(
                atPath: fileURL.path, contents: nil,
                attributes: [.protectionKey: FileProtectionType.completeUntilFirstUserAuthentication]
            )
            Self.exclude(fileURL)
        }
        if let handle = try? FileHandle(forWritingTo: fileURL) {
            defer { try? handle.close() }
            _ = try? handle.seekToEnd()
            try? handle.write(contentsOf: line)
        }
        fileBytes += line.count
        fileLines += 1
    }

    private func rewrite() {
        fileBytes = Self.write(entries, to: fileURL)
        fileLines = entries.count
    }

    private static func write(_ entries: [Entry], to url: URL) -> Int {
        var data = Data()
        for entry in entries { data.append(entry.line) }
        try? data.write(to: url, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
        exclude(url)
        return data.count
    }

    private static func prepare(_ directory: URL) {
        try? FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        exclude(directory)
    }

    private static func exclude(_ url: URL) {
        var values = URLResourceValues()
        values.isExcludedFromBackup = true
        var target = url
        try? target.setResourceValues(values)
    }

    private static func encoder() -> JSONEncoder {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        encoder.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
        return encoder
    }

    private static func encode(_ event: DiagnosticEvent) -> Data? {
        guard var data = try? encoder().encode(event) else { return nil }
        data.append(0x0A)
        return data
    }

    /// Lines that do not read back are skipped; every event read is checked again.
    private static func load(_ url: URL) -> [Entry] {
        guard let data = try? Data(contentsOf: url) else { return [] }
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        var result: [Entry] = []
        for slice in data.split(separator: 0x0A) {
            guard let event = try? decoder.decode(DiagnosticEvent.self, from: Data(slice)),
                  let clean = event.sanitized, let line = encode(clean) else { continue }
            result.append(Entry(event: clean, line: line))
        }
        return result
    }
}
