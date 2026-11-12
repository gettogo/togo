import SwiftUI
import TogoKit

/// The sheet for each `AppSheet`, built against the app's services.
struct RootSheet: View {
    @Environment(\.agentLibrary) private var agentLibrary
    @Environment(\.demoExit) private var demoExit
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
            .attachPrompts(store.services.attachPrompts, host: "sheet", active: true, nests: true)
            // Settings starts agent sign-ins in this window's store.
            .environment(\.signInStore, store)
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
        if let demoExit, sheet.changesMachine {
            // The demo machine has nothing to add, edit or sign in to.
            DemoNotHere(exit: demoExit) { store.sheet = nil }
        } else {
            forms
        }
    }

    @ViewBuilder private var forms: some View {
        switch sheet {
        case .addMachine:
            AddMachineSheet(store: store, services: onboarding, tutorial: tutorial)
        case .addMachineFilled(let draft):
            AddMachineSheet(flow: AddMachineFlow.filled(draft, store: store, services: onboarding, tutorial: tutorial))
        case .editMachine(let id):
            if let record = store.services.hostRecord(id) {
                AddMachineSheet(flow: AddMachineFlow.editing(record, store: store, services: onboarding, tutorial: tutorial))
            }
        case .setUpHere(let id):
            if let record = store.services.hostRecord(id) {
                AddMachineSheet(flow: AddMachineFlow.settingUp(record, store: store, services: onboarding, tutorial: tutorial))
            }
        case .newSession(let hostID):
            NewSessionSheet(
                model: NewSessionModel(
                    services: store.services, hostID: hostID, machines: store.controlCenter.machines,
                    library: agentLibrary ?? .inMemory(),
                    open: { hostID, sessionID, agent in
                        store.sheet = nil
                        store.open(hostID: hostID, sessionID: sessionID, agent: agent)
                    }
                ).prefilled(hostID.flatMap { store.takeNewSessionPrefill(for: $0) }),
                zoom: nil
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

private extension AppSheet {
    /// A form that adds, changes or signs in to a machine.
    var changesMachine: Bool {
        switch self {
        case .addMachine, .addMachineFilled, .editMachine, .setUpHere, .signIn: true
        case .newSession, .review, .settings: false
        }
    }
}

extension AddMachineFlow {
    /// The add-machine flow on the form step with `draft` already typed in, such as a host
    /// read from an SSH config.
    static func filled(
        _ draft: MachineDraft, store: AppStore, services: any OnboardingServices, tutorial: TutorialModel?
    ) -> AddMachineFlow {
        let flow = adding(store: store, services: services)
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
        stored(record, services: services, tutorial: tutorial, save: { try await store.services.saveHost($0) }) {
            store.sheet = nil
        }
    }

    /// Setting up a machine that came from another device: straight to connecting with this
    /// device's key. A machine without the key leads to adding it, then the helper check and
    /// the notification question, so this device pairs with the helper like the one that
    /// added it. Back reaches the form, for a user name that differs here.
    static func settingUp(
        _ record: HostRecord, store: AppStore, services: any OnboardingServices, tutorial: TutorialModel?
    ) -> AddMachineFlow {
        let flow = stored(record, services: services, tutorial: tutorial, save: { try await store.services.setUpHere($0) }) {
            store.sheet = nil
        }
        flow.draft.auth = .deviceKey
        flow.draft.importedKey = nil
        flow.notificationAsk = .asking(store.services)
        flow.path = [.connect]
        flow.retry()
        return flow
    }

    private static func stored(
        _ record: HostRecord, services: any OnboardingServices, tutorial: TutorialModel?,
        save: @escaping @MainActor (HostRecord) async throws -> Void, finish: @escaping @MainActor () -> Void
    ) -> AddMachineFlow {
        let flow = AddMachineFlow(services: services, id: record.id, save: save, finish: { _ in finish() })
        let profile = record.profile
        var draft = MachineDraft()
        draft.name = profile.name
        draft.address = profile.endpoint.host
        draft.port = String(profile.endpoint.port)
        draft.user = profile.endpoint.user
        draft.symbol = HostSymbol(rawValue: record.symbol) ?? .laptop
        draft.wake = record.wake
        draft.routes = profile.routes
        draft.lastRoute = profile.lastRoute
        draft.savedAddress = profile.endpoint.host
        draft.machineID = record.id
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
        flow.keepsNoHelper = record.noHelper
        return flow
    }
}
