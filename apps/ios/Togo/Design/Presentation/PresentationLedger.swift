import Foundation

/// The kind of surface a presentation puts on screen. Only the system ones: the coordinator
/// owns when they show, the system owns how they move.
enum PresentationKind: String, Sendable, CaseIterable {
    case sheet
    case cover
    case popover
    case inspector
    case alert
    case dialog

    /// Takes the window's one modal slot: one at a time, unless a policy nests one in another.
    var isModal: Bool { self == .sheet || self == .cover || self == .popover }
    /// A system question over whatever is up: one at a time, over any modal.
    var isTransient: Bool { self == .alert || self == .dialog }
}

/// One presentation a view asks for. `id` names the asking site (one per modifier instance);
/// `parent` is the presentation the asking view lives in, nil at the window's root.
struct PresentationRequest: Hashable, Sendable {
    typealias ID = String

    var id: ID
    /// What the logs call it: the site, such as "chat.stepDetail".
    var name: String
    var kind: PresentationKind
    var parent: ID?
    /// The policy lets this one stack over its parent (a question asked from inside a sheet that
    /// must stay up under it). Anything else asked from inside a sheet is a violation.
    var nests: Bool = false
    /// Presentations of one flow may replace each other (a sign-in sheet handing over to its
    /// outcome); nothing else may close another's presentation.
    var flow: String? = nil
    /// A side context that leaves the chat usable behind it (mission control's medium sheet): it
    /// steps aside for a modal asked for from behind it and comes back once that one has gone.
    var yields: Bool = false
}

/// Who closed a presentation.
enum PresentationDismissal: String, Sendable {
    /// The user: a swipe, Done, Cancel, a tap outside a popover.
    case user
    /// The flow that asked for it finished and withdrew it.
    case owner
    /// A later presentation of the same flow replaced it.
    case replaced
    /// A side context stepped aside for another modal; it shows again after that one.
    case yielded
}

/// A broken invariant: never two sheets stacked unless the policy nests them; a request never
/// vanishes; only the user or the owning flow closes a presentation.
struct PresentationViolation: Hashable, Sendable, CustomStringConvertible {
    enum Rule: String, Sendable {
        /// A modal was asked for from inside another modal that the policy does not nest it in.
        case stacked
        /// The view that asked went away while its presentation was up or waiting: SwiftUI took
        /// it down (or would never show it) because of a state change elsewhere.
        case hostVanished
        /// Someone other than the user or the owner closed it (a replace across flows).
        case foreignDismiss
        /// One site asked again for a different kind while its first was still up.
        case kindChanged
    }

    var rule: Rule
    var name: String
    var detail: String

    var description: String { "\(rule.rawValue): \(name) — \(detail)" }
}

/// The window's presentations as plain state: what shows, what waits, what is on its way out.
/// No SwiftUI, so every invariant is unit-tested. `PresentationCoordinator` drives it from views.
struct PresentationLedger: Sendable {
    enum Phase: String, Sendable {
        /// Waiting for the slot: another modal (or question) is up.
        case queued
        case shown
        /// Closed, its dismissal still animating; the slot frees once it has gone.
        case leaving
    }

    struct Entry: Hashable, Sendable {
        var request: PresentationRequest
        var phase: Phase
        var order: Int
    }

    private(set) var entries: [Entry] = []
    private(set) var violations: [PresentationViolation] = []
    /// Replaced by a later step of their flow: their owners set their state back.
    private(set) var retired: Set<PresentationRequest.ID> = []
    private(set) var dismissals: [(name: String, by: PresentationDismissal)] = []
    private var counter = 0
    private var suspended: Set<PresentationRequest.ID> = []

    // MARK: Reading

    func phase(of id: PresentationRequest.ID) -> Phase? { entries.first { $0.request.id == id }?.phase }

    func isShown(_ id: PresentationRequest.ID) -> Bool { phase(of: id) == .shown }

    var shown: [PresentationRequest] { entries.filter { $0.phase == .shown }.map(\.request) }

    var queued: [PresentationRequest] { entries.filter { $0.phase == .queued }.sorted { $0.order < $1.order }.map(\.request) }

    /// Modals shown or still leaving, outermost first.
    private var occupyingModals: [Entry] {
        entries.filter { $0.request.kind.isModal && $0.phase != .queued }.sorted { $0.order < $1.order }
    }

    /// The deepest modal on screen now.
    var topModal: PresentationRequest? {
        entries.filter { $0.request.kind.isModal && $0.phase == .shown }.max { $0.order < $1.order }?.request
    }

    /// How many sheets and popovers are stacked right now (1 is the rule; more only where the
    /// policy nests). A full-screen cover is a new root, so it does not count.
    var modalDepth: Int {
        entries.filter { $0.request.kind.isModal && $0.request.kind != .cover && $0.phase == .shown }.count
    }

    // MARK: Changes

    /// A view asks for its presentation. Answers whether it shows now.
    @discardableResult
    mutating func request(_ request: PresentationRequest) -> Bool {
        if let index = entries.firstIndex(where: { $0.request.id == request.id }) {
            let old = entries[index]
            if old.phase == .leaving {
                // Asked again while its last one is still going: wait for it to finish.
                entries.remove(at: index)
            } else {
                if old.request.kind != request.kind {
                    // A size change handing a sheet over to a popover is the same request; only
                    // a modal turning into a transient (or back) is a mistake.
                    if old.request.kind.isTransient != request.kind.isTransient {
                        record(.kindChanged, request.name, "\(old.request.kind.rawValue) → \(request.kind.rawValue)")
                    }
                    entries[index].request = request
                }
                return old.phase == .shown
            }
        }
        counter += 1
        entries.append(Entry(request: request, phase: .queued, order: counter))
        promote()
        return isShown(request.id)
    }

    /// The owner set its state back: the flow is done with it (or no longer wants it).
    mutating func withdraw(_ id: PresentationRequest.ID) {
        guard let index = entries.firstIndex(where: { $0.request.id == id }) else { return }
        switch entries[index].phase {
        case .queued:
            entries.remove(at: index)
            promote()
        case .leaving where suspended.contains(id):
            // It stepped aside and its owner closed it meanwhile: it does not come back.
            suspended.remove(id)
        case .shown:
            leave(index, by: .owner)
        case .leaving:
            break
        }
    }

    /// The system closed it while it was shown: the user swiped, tapped outside or chose a button.
    mutating func userDismissed(_ id: PresentationRequest.ID) {
        guard let index = entries.firstIndex(where: { $0.request.id == id }), entries[index].phase == .shown else { return }
        leave(index, by: .user)
    }

    /// Its dismissal finished (the content left the screen): the slot frees for what waits. A
    /// side context that stepped aside waits for its turn again.
    mutating func didLeave(_ id: PresentationRequest.ID) {
        guard let index = entries.firstIndex(where: { $0.request.id == id }), entries[index].phase == .leaving else { return }
        if suspended.contains(id) {
            suspended.remove(id)
            entries[index].phase = .queued
            // Behind what it stepped aside for.
            counter += 1
            entries[index].order = counter
        } else {
            entries.remove(at: index)
        }
        promote()
    }

    /// The owner of a replaced presentation set its state back.
    mutating func acknowledgeRetired(_ id: PresentationRequest.ID) {
        retired.remove(id)
    }

    /// The asking view went away. `stillWanted`: its state still asked for the presentation, so
    /// something other than the user or its flow ended it.
    mutating func hostGone(_ id: PresentationRequest.ID, stillWanted: Bool) {
        guard let index = entries.firstIndex(where: { $0.request.id == id }) else { return }
        let entry = entries[index]
        if stillWanted, entry.phase != .leaving {
            record(.hostVanished, entry.request.name,
                   entry.phase == .queued ? "its request was waiting and is now lost" : "taken down by a state change")
        }
        entries.remove(at: index)
        // Whatever it held up inside it went with it.
        let orphans = entries.filter { $0.request.parent == id }.map(\.request.id)
        for orphan in orphans { hostGone(orphan, stillWanted: false) }
        promote()
    }

    // MARK: Rules

    private mutating func leave(_ index: Int, by: PresentationDismissal) {
        entries[index].phase = .leaving
        dismissals.append((entries[index].request.name, by))
        // Questions and inspectors leave without holding anything up.
        if !entries[index].request.kind.isModal {
            let id = entries[index].request.id
            didLeave(id)
        }
    }

    /// Shows what waits, in the order it asked, while the rules allow.
    private mutating func promote() {
        var changed = true
        while changed {
            changed = false
            for entry in entries.filter({ $0.phase == .queued }).sorted(by: { $0.order < $1.order }) {
                if admit(entry.request) {
                    if let index = entries.firstIndex(where: { $0.request.id == entry.request.id }) {
                        entries[index].phase = .shown
                        changed = true
                    }
                }
            }
        }
    }

    /// Whether `request` may show now, replacing a same-flow sheet on its way.
    private mutating func admit(_ request: PresentationRequest) -> Bool {
        switch request.kind {
        case .inspector:
            return true
        case .alert, .dialog:
            return !entries.contains { $0.request.kind.isTransient && $0.phase != .queued && $0.request.id != request.id }
        case .sheet, .cover, .popover:
            let occupying = occupyingModals.filter { $0.request.id != request.id }
            guard let top = occupying.last else { return true }
            // Asked from inside the modal on top: stacking it is the only way it can show.
            if request.parent == top.request.id, top.phase == .shown {
                // A full-screen cover is a new root (the workspace): one sheet over it is the rule.
                if !request.nests, top.request.kind != .cover {
                    record(.stacked, request.name, "asked from inside \(top.request.name); one sheet at a time (C8)")
                }
                return true
            }
            // A modal that is still leaving holds the slot until it has gone.
            guard top.phase == .shown else { return false }
            guard !isAncestor(top.request.id, of: request),
                  let index = entries.firstIndex(where: { $0.request.id == top.request.id }) else { return false }
            // The same flow hands over: the old one leaves, the new one follows it.
            if let flow = request.flow, top.request.flow == flow {
                entries[index].phase = .leaving
                retired.insert(top.request.id)
                dismissals.append((top.request.name, .replaced))
            } else if top.request.yields, !request.yields {
                entries[index].phase = .leaving
                suspended.insert(top.request.id)
                dismissals.append((top.request.name, .yielded))
            }
            return false
        }
    }

    private func isAncestor(_ id: PresentationRequest.ID, of request: PresentationRequest) -> Bool {
        var parent = request.parent
        while let current = parent {
            if current == id { return true }
            parent = entries.first { $0.request.id == current }?.request.parent
        }
        return false
    }

    private mutating func record(_ rule: PresentationViolation.Rule, _ name: String, _ detail: String) {
        violations.append(PresentationViolation(rule: rule, name: name, detail: detail))
    }
}
