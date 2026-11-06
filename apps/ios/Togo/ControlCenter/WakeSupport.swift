import Foundation
import TogoKit

/// Sending a wake packet needs the multicast networking entitlement. Until the app has it,
/// the Wake button stays out of builds; compile with `TOGO_WAKE_ON_LAN` to show it.
enum WakeFeature {
    static var isEnabled: Bool {
        #if TOGO_WAKE_ON_LAN
        true
        #else
        false
        #endif
    }
}

enum WakeCopy {
    static let button = String(localized: "wake.button", defaultValue: "Wake")

    static func sent(_ machine: String) -> String {
        String(localized: "wake.sent", defaultValue: "Sent a wake signal. This works on the same network as \(machine).")
    }

    static func off(_ machine: String) -> String {
        String(localized: "wake.off", defaultValue: "Wake for network access is off on \(machine).")
    }

    static func failed(_ machine: String) -> String {
        String(localized: "wake.failed", defaultValue: "Couldn't send a wake signal to \(machine). Check that you are on the same network.")
    }

    static let formLabel = String(localized: "wake.form.label", defaultValue: "Wake address")

    /// The button shows on an offline machine that has a stored target.
    static func showsButton(for machine: MachineSection, enabled: Bool) -> Bool {
        guard enabled, machine.wake != nil else { return false }
        if case .offline = machine.reachability { return true }
        return false
    }
}

/// Keeps a machine's stored wake target in step with what its helper reports.
enum WakeLearning {
    /// The record with the target from `snapshot`, or nil when nothing changes: the machine
    /// has no host info capability, reported no usable interface, or reported what is stored.
    static func updated(_ record: HostRecord, from snapshot: HostSnapshot) -> HostRecord? {
        guard snapshot.capabilities.contains(Capability.hostInfo), let info = snapshot.hostInfo,
              let target = WakeTarget(from: info), target != record.wake
        else { return nil }
        var record = record
        record.wake = target
        return record
    }
}

extension HostRecord {
    /// The record as the monitor sees it: a changed wake target does not restart a connection.
    var withoutWake: HostRecord {
        var copy = self
        copy.wake = nil
        return copy
    }
}
