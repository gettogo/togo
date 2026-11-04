import Foundation
import MoshShim
import Synchronization

/// One mosh_main session on its own thread, plus the thread that drains its screen pipe.
///
/// mosh is controlled with signals (SIGWINCH to resize, SIGTERM to quit), so it needs a
/// real pthread, never a pool thread. Ownership of the C handles: the mosh thread owns
/// the keystroke pipe's read end and the screen pipe's write end (the shim closes both);
/// this object owns the keystroke write end; the reader thread owns the screen read end
/// and closes it at EOF. `window` is read by mosh until mosh_main returns and is freed
/// in deinit, which cannot run before then because both threads retain this object.
final class MoshClient: @unchecked Sendable {
    private let inputWrite: Int32
    private let inputRead: Int32
    private let outputRead: Int32
    private let outputWrite: Int32
    private let window: UnsafeMutablePointer<winsize>
    private let control = Mutex<Control>(Control())
    private let moshReturned = DispatchSemaphore(value: 0)

    private struct Control {
        var thread: pthread_t?
        var inputOpen = true
        var status: Int32 = 0
    }

    init(cols: Int, rows: Int) throws {
        var input: [Int32] = [0, 0]
        var output: [Int32] = [0, 0]
        guard pipe(&input) == 0 else { throw POSIXError(.init(rawValue: errno) ?? .EIO) }
        guard pipe(&output) == 0 else {
            close(input[0])
            close(input[1])
            throw POSIXError(.init(rawValue: errno) ?? .EIO)
        }
        (inputRead, inputWrite) = (input[0], input[1])
        (outputRead, outputWrite) = (output[0], output[1])
        // A write after mosh has closed its end must fail with EPIPE, not kill the app.
        _ = fcntl(inputWrite, F_SETNOSIGPIPE, 1)
        _ = fcntl(inputWrite, F_SETFL, fcntl(inputWrite, F_GETFL) | O_NONBLOCK)
        window = .allocate(capacity: 1)
        window.initialize(to: winsize(ws_row: UInt16(clamping: rows), ws_col: UInt16(clamping: cols), ws_xpixel: 0, ws_ypixel: 0))
    }

    deinit {
        if control.withLock({ $0.inputOpen }) { close(inputWrite) }
        window.deallocate()
    }

    /// `deliver` runs once per chunk and the reader waits for it, so a slow consumer
    /// stops the reads and, once the pipe fills, mosh itself. `exited` runs once, after
    /// mosh_main has returned and all its output has been delivered.
    func start(
        host: String, port: Int, key: String, predict: String,
        deliver: @escaping @Sendable (Data) async -> Void,
        exited: @escaping @Sendable (Int32) -> Void
    ) {
        let session = Thread { [self] in
            control.withLock { $0.thread = pthread_self() }
            let status = togo_mosh_run(inputRead, outputWrite, window, host, String(port), key, predict)
            control.withLock {
                $0.status = status
                $0.thread = nil
            }
            moshReturned.signal()
        }
        session.name = "mosh \(host):\(port)"
        session.stackSize = 1 << 20

        let reader = Thread { [self] in
            drainOutput(deliver)
            close(outputRead)
            moshReturned.wait()
            exited(control.withLock { $0.status })
        }
        reader.name = "mosh output"
        session.start()
        reader.start()
    }

    func send(_ data: Data) async throws {
        var remaining = data[...]
        while !remaining.isEmpty {
            // The write happens under the same lock closeInput takes, so the descriptor
            // cannot be closed and reused between the check and the write. O_NONBLOCK
            // keeps the lock from being held while the pipe is full.
            let result: Result<Int, POSIXError> = control.withLock { control in
                guard control.inputOpen else { return .failure(POSIXError(.EPIPE)) }
                let written = remaining.withUnsafeBytes { write(inputWrite, $0.baseAddress, $0.count) }
                if written >= 0 { return .success(written) }
                if errno == EAGAIN { return .success(0) }
                return .failure(POSIXError(.init(rawValue: errno) ?? .EIO))
            }
            let written = try result.get()
            if written > 0 {
                remaining = remaining.dropFirst(written)
            } else {
                try await Task.sleep(for: .milliseconds(5))
            }
        }
    }

    /// mosh reads `window` without a lock, but only after SIGWINCH, so it sees the new
    /// values once the signal below is delivered.
    func resize(cols: Int, rows: Int) {
        window.pointee.ws_col = UInt16(clamping: cols)
        window.pointee.ws_row = UInt16(clamping: rows)
        signal(SIGWINCH)
    }

    /// EOF on the keystroke pipe makes mosh tell the server to shut down, then return.
    func closeInput() {
        control.withLock { control in
            guard control.inputOpen else { return }
            control.inputOpen = false
            close(inputWrite)
        }
    }

    /// Only for a session past its startup frame: before mosh installs its handler,
    /// SIGTERM's default action would end the whole process.
    func terminate() {
        signal(SIGTERM)
    }

    private func signal(_ signal: Int32) {
        control.withLock { control in
            if let thread = control.thread { _ = togo_mosh_signal(thread, signal) }
        }
    }

    private func drainOutput(_ deliver: @escaping @Sendable (Data) async -> Void) {
        let delivered = DispatchSemaphore(value: 0)
        var buffer = [UInt8](repeating: 0, count: 64 << 10)
        while true {
            let count = buffer.withUnsafeMutableBytes { read(outputRead, $0.baseAddress, $0.count) }
            if count < 0, errno == EINTR { continue }
            guard count > 0 else { return }
            let chunk = Data(buffer[..<count])
            Task {
                await deliver(chunk)
                delivered.signal()
            }
            delivered.wait()
        }
    }
}
