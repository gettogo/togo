import SwiftUI
import TogoKit

/// The sheet for each `AppSheet`, built against the app's services.
struct RootSheet: View {
    @Environment(\.agentLibrary) private var agentLibrary
    let sheet: AppSheet
    let store: AppStore
    let onboarding: any OnboardingServices
    let zoom: Namespace.ID
    var tutorial: TutorialModel?

    var settings: (() -> AnyView)?

    var body: some View {
        presented
            // New session asks for agents and folders; if that needs a password or code, the
            // question shows over this sheet.
            .attachPrompts(store.services.attachPrompts, active: true)
    }

    /// New session sets its own heights, since browsing folders takes it to full height.
    @ViewBuilder private var presented: some View {
        switch sheet {
        case .newSession: content
        case .signIn: content.togoSheet(.prompt)
        case .settings: content.togoSheet(.page)
        default: content.togoSheet(.form)
        }
    }

    @ViewBuilder private var content: some View {
        switch sheet {
        case .addMachine:
            AddMachineSheet(store: store, services: onboarding, tutorial: tutorial)
        case .addMachineFilled(let draft):
            AddMachineSheet(flow: AddMachineFlow.filled(draft, store: store, services: onboarding, tutorial: tutorial))
        case .editMachine(let id):
            if let record = store.services.hostRecord(id) {
                AddMachineSheet(flow: AddMachineFlow.editing(record, store: store, services: onboarding, tutorial: tutorial))
            }
        case .newSession(let hostID):
            NewSessionSheet(
                model: NewSessionModel(
                    services: store.services, hostID: hostID, machines: store.controlCenter.machines,
                    library: agentLibrary ?? .inMemory(),
                    open: { hostID, sessionID in
                        store.sheet = nil
                        store.open(hostID: hostID, sessionID: sessionID)
                    }
                ).prefilled(hostID.flatMap { store.takeNewSessionPrefill(for: $0) }),
                zoom: hostID == nil && store.newSessionZooms
                    ? .init(sourceID: ControlCenterView.newSessionSourceID, namespace: zoom) : nil
            )
        case .signIn(let id):
            SignInSheet(model: SignInModel(
                hostID: id, machine: store.machineName(for: id), services: store.services,
                finish: { store.sheet = nil }
            ))
        case .settings:
            settings?()
        case .review:
            // RootView presents the review itself, as a cover on a phone.
            EmptyView()
        }
    }
}

extension AddMachineFlow {
    /// The add-machine flow on the form step with `draft` already typed in, such as a host
    /// read from an SSH config.
    static func filled(
        _ draft: MachineDraft, store: AppStore, services: any OnboardingServices, tutorial: TutorialModel?
    ) -> AddMachineFlow {
        let flow = AddMachineFlow(
            services: services,
            save: { try await store.services.saveHost($0) },
            finish: { _ in store.sheet = nil }
        )
        if let tutorial { flow.onDetected = { tutorial.record($0) } }
        flow.notificationAsk = .asking(store.services)
        flow.draft = draft
        flow.startsOnForm = true
        return flow
    }

    /// The add-machine flow on the form step, filled in from a stored machine. Saving replaces
    /// the record under the same id, so its monitor starts over with the new details.
    static func editing(
        _ record: HostRecord, store: AppStore, services: any OnboardingServices, tutorial: TutorialModel?
    ) -> AddMachineFlow {
        let flow = AddMachineFlow(
            services: services, id: record.id,
            save: { try await store.services.saveHost($0) },
            finish: { _ in store.sheet = nil }
        )
        let profile = record.profile
        var draft = MachineDraft()
        draft.name = profile.name
        draft.address = profile.endpoint.host
        draft.port = String(profile.endpoint.port)
        draft.user = profile.endpoint.user
        draft.symbol = HostSymbol(rawValue: record.symbol) ?? .laptop
        draft.wake = record.wake
        if let jump = profile.jumps.first {
            draft.jump = MachineDraft.jumpText(jump)
        }
        switch profile.auth {
        case .password:
            draft.auth = .password
        case .key(let id) where id == LiveOnboardingServices.storedDeviceKeyID:
            draft.auth = .deviceKey
        case .key(let id):
            // A key named by a machine synced from another device may not be here; the form
            // then asks for one instead of showing a key it cannot use.
            draft.auth = .importedKey
            draft.importedKey = services.keyLabel(id: id).map { ImportedKey(id: id, label: $0) }
        }
        if let tutorial { flow.onDetected = { tutorial.record($0) } }
        flow.draft = draft
        flow.startsOnForm = true
        return flow
    }
}
