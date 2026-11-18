import Foundation
import Synchronization
import os

/// An array of Codable values stored as one JSON file.
///
/// Every read-modify-write holds `flock` on a lock file next to the data, so the app, the
/// notification service and the action handler, each a separate process, never lose each
/// other's writes. The data file is replaced atomically, so a reader never sees half of it.
final class SharedBlob<Value: Codable & Sendable>: Sendable {
    private static var logger: Logger { Logger(subsystem: "app.togo.push", category: "SharedBlob") }
    private let dataURL: URL
    private let lockURL: URL
    private let mutex = Mutex(())

    init(directory: URL, name: String) {
        try? FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        dataURL = directory.appending(path: "\(name).json")
        lockURL = directory.appending(path: "\(name).lock")
    }

    func read() -> [Value] {
        mutex.withLock { _ in
            withFileLock(LOCK_SH) { load() ?? [] } ?? []
        }
    }

    func update(_ change: (inout [Value]) -> Void) {
        mutex.withLock { _ in
            withFileLock(LOCK_EX) {
                guard var values = load() else { return }
                change(&values)
                store(values)
            }
        }
    }

    private func load() -> [Value]? {
        do {
            return try JSONDecoder().decode([Value].self, from: Data(contentsOf: dataURL))
        } catch let error as CocoaError where error.code == .fileReadNoSuchFile {
            return []
        } catch {
            // An unavailable or newer record is not an empty store. In particular, a
            // mutation must never replace it with a list derived from an empty fallback.
            Self.logger.error("Shared data could not be read; changes were skipped.")
            return nil
        }
    }

    private func store(_ values: [Value]) {
        guard let data = try? JSONEncoder().encode(values) else { return }
        #if os(macOS)
        try? data.write(to: dataURL, options: .atomic)
        #else
        // Readable after first unlock, like the device keys, so the notification service can
        // use it on a locked phone.
        try? data.write(to: dataURL, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
        #endif
    }

    /// Runs `body` holding `flock(operation)` on the lock file. `flock` locks belong to the
    /// open file description, so separate opens exclude each other even in one process.
    private func withFileLock<T>(_ operation: Int32, _ body: () -> T) -> T? {
        let fd = open(lockURL.path, O_RDWR | O_CREAT | O_CLOEXEC, 0o600)
        guard fd >= 0 else {
            Self.logger.error("Shared data lock could not be opened; access was skipped.")
            return nil
        }
        defer { close(fd) }
        var status: Int32
        repeat { status = flock(fd, operation) } while status != 0 && errno == EINTR
        guard status == 0 else {
            Self.logger.error("Shared data could not be locked; access was skipped.")
            return nil
        }
        defer { flock(fd, LOCK_UN) }
        return body()
    }
}
