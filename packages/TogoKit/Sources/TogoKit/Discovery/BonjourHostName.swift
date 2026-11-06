import Foundation
import dnssd

/// Asks mDNS for the host name a Bonjour service runs on, such as `studio.local.`. Network
/// reports only the address a connection reached, which on a Mac is often a link-local IPv6
/// address; the host name is what the user would type.
final class BonjourHostName: @unchecked Sendable {
    // Every property is read and written on `queue` only.
    private let queue = DispatchQueue(label: "app.togo.bonjour.hostname")
    private var ref: DNSServiceRef?
    private var context: Unmanaged<BonjourHostName>?
    private var continuation: CheckedContinuation<Target?, Never>?

    struct Target: Sendable {
        var host: String
        var port: Int
    }

    private init() {}

    /// The service's host name and port, or nil if mDNS gives none within `timeout`.
    static func lookUp(name: String, type: String, domain: String, timeout: Duration) async -> Target? {
        let lookup = BonjourHostName()
        let seconds = Double(timeout.components.seconds) + Double(timeout.components.attoseconds) / 1e18
        return await withCheckedContinuation { continuation in
            lookup.queue.async {
                lookup.start(name: name, type: type, domain: domain, continuation: continuation)
            }
            lookup.queue.asyncAfter(deadline: .now() + seconds) { lookup.finish(nil) }
        }
    }

    private func start(name: String, type: String, domain: String, continuation: CheckedContinuation<Target?, Never>) {
        self.continuation = continuation
        let context = Unmanaged.passRetained(self)
        self.context = context
        var ref: DNSServiceRef?
        let error = DNSServiceResolve(
            &ref, 0, 0, name, type, domain,
            { _, _, _, errorCode, _, hostTarget, port, _, _, context in
                guard let context else { return }
                let lookup = Unmanaged<BonjourHostName>.fromOpaque(context).takeUnretainedValue()
                guard errorCode == kDNSServiceErr_NoError, let hostTarget else {
                    lookup.finish(nil)
                    return
                }
                // The port arrives in network byte order.
                lookup.finish(Target(host: String(cString: hostTarget), port: Int(UInt16(bigEndian: port))))
            },
            context.toOpaque()
        )
        guard error == kDNSServiceErr_NoError, let ref else {
            finish(nil)
            return
        }
        self.ref = ref
        DNSServiceSetDispatchQueue(ref, queue)
    }

    /// Called on `queue`: by the reply, the timeout, or a failed start. Only the first counts.
    private func finish(_ target: Target?) {
        guard let continuation else { return }
        self.continuation = nil
        if let ref {
            // This can run inside the reply callback. That is allowed: the ref was handed to
            // `queue` with DNSServiceSetDispatchQueue, and this runs on that same queue, so no
            // other callback for it can be in flight.
            DNSServiceRefDeallocate(ref)
            self.ref = nil
        }
        context?.release()
        context = nil
        continuation.resume(returning: target)
    }
}
