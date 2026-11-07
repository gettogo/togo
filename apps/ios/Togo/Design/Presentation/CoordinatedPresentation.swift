import SwiftUI

/// What a presented surface's content gets: it lives in its presentation (so what it presents in
/// turn knows its parent) and tells the coordinator when it has left the screen.
@MainActor struct PresentedContent {
    fileprivate let id: PresentationRequest.ID
    fileprivate let coordinator: PresentationCoordinator?

    func callAsFunction<V: View>(_ content: V) -> some View {
        content
            .environment(\.presentationParent, id)
            .onDisappear { [coordinator, id] in coordinator?.didLeave(id) }
            .presentationProbe()
    }
}

/// The bookkeeping every coordinated presentation shares. The asking view's state (`wanted`)
/// says what it wants; the coordinator says whether it shows (`shown`). A close from the system
/// while shown is the user's; setting the state back is the owner's.
struct CoordinatedSlot<Base: View, Presented: View>: View {
    @Environment(\.presentationCoordinator) private var coordinator
    @Environment(\.presentationParent) private var parent
    @State private var id = UUID().uuidString

    let base: Base
    let name: String
    let kind: PresentationKind
    let nests: Bool
    let flow: String?
    let yields: Bool
    let wanted: Bool
    /// Sets the asking view's state back after the system closed the presentation.
    let release: () -> Void
    let present: (Base, _ shown: Bool, _ closed: @escaping () -> Void, _ inside: PresentedContent) -> Presented

    init(
        _ base: Base, name: String, kind: PresentationKind, nests: Bool = false, flow: String? = nil, yields: Bool = false,
        wanted: Bool, release: @escaping () -> Void,
        present: @escaping (Base, _ shown: Bool, _ closed: @escaping () -> Void, _ inside: PresentedContent) -> Presented
    ) {
        self.base = base
        self.name = name
        self.kind = kind
        self.nests = nests
        self.flow = flow
        self.yields = yields
        self.wanted = wanted
        self.release = release
        self.present = present
    }

    var body: some View {
        let inside = PresentedContent(id: id, coordinator: coordinator)
        if let coordinator {
            let request = PresentationRequest(
                id: id, name: name, kind: kind, parent: parent, nests: nests, flow: flow, yields: yields
            )
            let shown = wanted && coordinator.isShown(id)
            present(base, shown, { [coordinator, id, release] in
                if coordinator.ledger.phase(of: id) == .shown { coordinator.userDismissed(id) }
                release()
            }, inside)
            .onChange(of: wanted, initial: true) { _, wanted in
                if wanted { coordinator.request(request) } else { coordinator.withdraw(id) }
            }
            .onChange(of: kind) { _, _ in
                if wanted { coordinator.request(request) }
            }
            // A later step of its flow replaced it: the flow's state lets go too.
            .onChange(of: coordinator.ledger.retired.contains(id)) { _, retired in
                guard retired else { return }
                coordinator.acknowledgeRetired(id)
                release()
            }
            .onAppear { if wanted { coordinator.request(request) } }
            .onDisappear { [wanted] in coordinator.hostGone(id, stillWanted: wanted) }
        } else {
            // Outside a window (previews, snapshot tests): as the state says.
            present(base, wanted, release, inside)
        }
    }
}

extension View {
    /// A sheet the window's coordinator owns: `.sheet(isPresented:)` with the coordinator's
    /// rules (one at a time, never lost, closed only by the user or the flow).
    ///
    /// `yields`: a side context (a sheet that leaves the content behind it usable) that steps
    /// aside for a modal asked for from behind it and comes back after.
    func presentSheet<Shown: View>(
        _ name: String, isPresented: Binding<Bool>, nests: Bool = false, flow: String? = nil, yields: Bool = false,
        onDismiss: (() -> Void)? = nil, @ViewBuilder content: @escaping () -> Shown
    ) -> some View {
        CoordinatedSlot(
            self, name: name, kind: .sheet, nests: nests, flow: flow, yields: yields, wanted: isPresented.wrappedValue,
            release: { if isPresented.wrappedValue { isPresented.wrappedValue = false } }
        ) { view, shown, closed, inside in
            view.sheet(isPresented: Binding(get: { shown }, set: { if !$0 { closed() } }), onDismiss: onDismiss) {
                inside(content())
            }
        }
    }

    /// `presentSheet` for an item.
    func presentSheet<Item: Identifiable, Shown: View>(
        _ name: String, item: Binding<Item?>, nests: Bool = false, flow: String? = nil,
        onDismiss: (() -> Void)? = nil, @ViewBuilder content: @escaping (Item) -> Shown
    ) -> some View {
        CoordinatedSlot(
            self, name: name, kind: .sheet, nests: nests, flow: flow, wanted: item.wrappedValue != nil,
            release: { if item.wrappedValue != nil { item.wrappedValue = nil } }
        ) { view, shown, closed, inside in
            view.sheet(item: Binding(get: { shown ? item.wrappedValue : nil }, set: { if $0 == nil { closed() } }),
                       onDismiss: onDismiss) { value in
                inside(content(value))
            }
        }
    }

    /// A full-screen cover the window's coordinator owns.
    func presentCover<Shown: View>(
        _ name: String, isPresented: Binding<Bool>, nests: Bool = false, flow: String? = nil,
        onDismiss: (() -> Void)? = nil, @ViewBuilder content: @escaping () -> Shown
    ) -> some View {
        CoordinatedSlot(
            self, name: name, kind: .cover, nests: nests, flow: flow, wanted: isPresented.wrappedValue,
            release: { if isPresented.wrappedValue { isPresented.wrappedValue = false } }
        ) { view, shown, closed, inside in
            view.fullScreenCover(isPresented: Binding(get: { shown }, set: { if !$0 { closed() } }), onDismiss: onDismiss) {
                inside(content())
            }
        }
    }

    /// `presentCover` for an item.
    func presentCover<Item: Identifiable, Shown: View>(
        _ name: String, item: Binding<Item?>, nests: Bool = false, flow: String? = nil,
        onDismiss: (() -> Void)? = nil, @ViewBuilder content: @escaping (Item) -> Shown
    ) -> some View {
        CoordinatedSlot(
            self, name: name, kind: .cover, nests: nests, flow: flow, wanted: item.wrappedValue != nil,
            release: { if item.wrappedValue != nil { item.wrappedValue = nil } }
        ) { view, shown, closed, inside in
            view.fullScreenCover(item: Binding(get: { shown ? item.wrappedValue : nil }, set: { if $0 == nil { closed() } }),
                                 onDismiss: onDismiss) { value in
                inside(content(value))
            }
        }
    }

    /// Any other system presentation (an alert, a confirmation dialog, a popover, an inspector)
    /// under the coordinator: `present` attaches it with the binding the coordinator gates.
    ///
    ///     .coordinated("diff.discard", .dialog, isPresented: $confirming) { view, shown in
    ///         view.confirmationDialog(title, isPresented: shown) { … }
    ///     }
    func coordinated<Shown: View>(
        _ name: String, _ kind: PresentationKind, isPresented: Binding<Bool>, nests: Bool = true,
        present: @escaping (Self, Binding<Bool>) -> Shown
    ) -> some View {
        CoordinatedSlot(
            self, name: name, kind: kind, nests: nests, wanted: isPresented.wrappedValue,
            release: { if isPresented.wrappedValue { isPresented.wrappedValue = false } }
        ) { view, shown, closed, _ in
            present(view, Binding(get: { shown }, set: { if !$0 { closed() } }))
        }
    }
}
