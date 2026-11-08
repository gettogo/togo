import Observation
import OSLog
import SwiftUI
import UIKit

/// The one owner of a window's sheets, covers, popovers, inspectors, alerts and dialogs.
/// Views still declare what they present (`presentSheet`, `presentCover`, `coordinated`); the
/// coordinator decides when it shows:
///
/// - never two modals stacked, unless the policy nests one (`PresentationRequest.nests`);
/// - a request that cannot show yet waits until it can, it never silently vanishes;
/// - only the user or the flow that asked closes a presentation (or a later step of the same
///   flow replaces it);
/// - the style comes from `AdaptivePresentation` (content class × width), never the device.
///
/// Every broken rule is logged (`violations`, the `presentation` log category) and, in DEBUG with
/// `TOGO_PRESENTATION_STRICT=1`, stops at an assertion.
@MainActor @Observable final class PresentationCoordinator {
    private(set) var ledger = PresentationLedger()
    @ObservationIgnored private var leaveTimers: [PresentationRequest.ID: Task<Void, Never>] = [:]
    @ObservationIgnored private let log = Logger(subsystem: "app.togo", category: "presentation")
    /// Called for each broken rule, after it is logged. Tests collect them here.
    @ObservationIgnored var onViolation: ((PresentationViolation) -> Void)?
    @ObservationIgnored private let strict: Bool

    /// How long a dismissal may take before its slot frees anyway (a host that never reported).
    static let leaveTimeout: Duration = .milliseconds(700)

    init(strict: Bool = PresentationCoordinator.strictFromEnvironment) {
        self.strict = strict
    }

    static var strictFromEnvironment: Bool {
        #if DEBUG
        ProcessInfo.processInfo.environment["TOGO_PRESENTATION_STRICT"] == "1"
        #else
        false
        #endif
    }

    var violations: [PresentationViolation] { ledger.violations }

    func isShown(_ id: PresentationRequest.ID) -> Bool { ledger.isShown(id) }

    // MARK: From the presenting views

    func request(_ request: PresentationRequest) {
        mutate { $0.request(request) }
    }

    func withdraw(_ id: PresentationRequest.ID) {
        mutate { $0.withdraw(id) }
    }

    func userDismissed(_ id: PresentationRequest.ID) {
        mutate { $0.userDismissed(id) }
    }

    func didLeave(_ id: PresentationRequest.ID) {
        leaveTimers.removeValue(forKey: id)?.cancel()
        mutate { $0.didLeave(id) }
    }

    func acknowledgeRetired(_ id: PresentationRequest.ID) {
        mutate { $0.acknowledgeRetired(id) }
    }

    func hostGone(_ id: PresentationRequest.ID, stillWanted: Bool) {
        leaveTimers.removeValue(forKey: id)?.cancel()
        mutate { $0.hostGone(id, stillWanted: stillWanted) }
    }

    // MARK: Policy

    /// The style for `content` in the room the window has: `AdaptivePresentation`'s table.
    func style(
        for content: PresentationContent, sizeClass: UserInterfaceSizeClass?, containerWidth: CGFloat?,
        anchored: Bool = false, hostsInspector: Bool = false
    ) -> PresentationStyle {
        AdaptivePresentation.style(
            for: content, sizeClass: sizeClass, containerWidth: containerWidth,
            anchored: anchored, hostsInspector: hostsInspector
        )
    }

    /// The kind of surface a style is.
    static func kind(of style: PresentationStyle) -> PresentationKind {
        switch style {
        case .popover: .popover
        case .inspector: .inspector
        case .confirmationDialog: .dialog
        default: .sheet
        }
    }

    // MARK: Internals

    private func mutate(_ change: (inout PresentationLedger) -> Void) {
        let before = ledger.violations.count
        let leavingBefore = Set(ledger.entries.filter { $0.phase == .leaving }.map(\.request.id))
        var next = ledger
        change(&next)
        ledger = next
        for violation in ledger.violations.dropFirst(before) { report(violation) }
        // A dismissal the host never reports still frees the slot.
        for entry in ledger.entries where entry.phase == .leaving && !leavingBefore.contains(entry.request.id) {
            let id = entry.request.id
            leaveTimers[id]?.cancel()
            leaveTimers[id] = Task { [weak self] in
                try? await Task.sleep(for: Self.leaveTimeout)
                guard !Task.isCancelled else { return }
                self?.didLeave(id)
            }
        }
    }

    private func report(_ violation: PresentationViolation) {
        log.fault("presentation rule broken: \(violation.description, privacy: .public)")
        onViolation?(violation)
        #if DEBUG
        if strict { assertionFailure("Presentation rule broken: \(violation)") }
        #endif
    }

    /// One line for UI tests: `violations=N depth=D shown=a,b queued=c`.
    var summary: String {
        let shown = ledger.shown.map(\.name).joined(separator: ",")
        let queued = ledger.queued.map(\.name).joined(separator: ",")
        let rules = ledger.violations.map { "\($0.rule.rawValue):\($0.name)" }.joined(separator: ",")
        return "violations=\(ledger.violations.count) depth=\(ledger.modalDepth) shown=\(shown) queued=\(queued) broken=\(rules)"
    }
}

extension EnvironmentValues {
    /// The window's presentation coordinator. nil outside a window (previews, snapshot tests),
    /// where presentations show as their state says, uncoordinated.
    @Entry var presentationCoordinator: PresentationCoordinator?
    /// The presentation this view lives in; nil at the window's root.
    @Entry var presentationParent: PresentationRequest.ID? = nil
}

/// Each window scene's coordinator, for presenters outside SwiftUI's environment (`LinkPresenter`).
@MainActor
enum PresentationRegistry {
    private static var coordinators: [ObjectIdentifier: WeakCoordinator] = [:]

    private struct WeakCoordinator {
        weak var scene: UIWindowScene?
        weak var coordinator: PresentationCoordinator?
    }

    static func register(_ coordinator: PresentationCoordinator, for scene: UIWindowScene) {
        coordinators = coordinators.filter { $0.value.scene != nil && $0.value.coordinator != nil }
        coordinators[ObjectIdentifier(scene)] = WeakCoordinator(scene: scene, coordinator: coordinator)
    }

    static func coordinator(for scene: UIWindowScene?) -> PresentationCoordinator? {
        guard let scene else { return nil }
        return coordinators[ObjectIdentifier(scene)]?.coordinator
    }
}
