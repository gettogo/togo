import Foundation
import Synchronization

/// An array of Codable values stored as one JSON file.
///
/// Every read-modify-write holds `flock` on a lock file next to the data, so the app, the
/// notification service and the action handler, each a separate process, never lose each
/// other's writes. The data file is replaced atomically, so a reader never sees half of it.
final class SharedBlob<Value: Codable & Sendable>: Sendable {
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
            withFileLock(LOCK_SH) { load() }
        }
    }

    func update(_ change: (inout [Value]) -> Void) {
        mutex.withLock { _ in
            withFileLock(LOCK_EX) {
                var values = load()
                change(&values)
                store(values)
            }
        }
    }

    private func load() -> [Value] {
        guard let data = try? Data(contentsOf: dataURL) else { return [] }
        return (try? JSONDecoder().decode([Value].self, from: data)) ?? []
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
    private func withFileLock<T>(_ operation: Int32, _ body: () -> T) -> T {
        let fd = open(lockURL.path, O_RDWR | O_CREAT | O_CLOEXEC, 0o600)
        guard fd >= 0 else { return body() }
        defer { close(fd) }
        while flock(fd, operation) != 0, errno == EINTR {}
        defer { flock(fd, LOCK_UN) }
        return body()
    }
}
