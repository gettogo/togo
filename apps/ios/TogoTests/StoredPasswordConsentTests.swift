import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct StoredPasswordConsentTests {
    @Test func unavailableMetadataDoesNotEraseAnExistingPasswordOnRename() throws {
        let id = UUID()
        let record = HostRecord(id: id, profile: HostProfile(
            id: id, name: "test", endpoint: HostEndpoint(host: "example.test", user: "test"), auth: .password
        ), symbol: "laptopcomputer")
        let services = FakeOnboardingServices()
        services.storedPasswords.insert(id)
        services.passwordCheckFails = true
        let store = AppStore(services: FakeAppServices())
        let flow = AddMachineFlow.editing(record, store: store, services: services, tutorial: nil)
        flow.draft.name = "renamed"
        #expect(!flow.draft.savePassword)
        try flow.savePassword(for: id)
        #expect(services.storedPasswords.contains(id))
        #expect(services.savedPasswords.isEmpty)
    }

    @Test func editingDoesNotInventPasswordStorageConsent() {
        let id = UUID()
        let record = HostRecord(id: id, profile: HostProfile(
            id: id, name: "test", endpoint: HostEndpoint(host: "example.test", user: "test"), auth: .password
        ), symbol: "laptopcomputer")
        let services = FakeOnboardingServices()
        let store = AppStore(services: FakeAppServices())
        let notSaved = AddMachineFlow.editing(record, store: store, services: services, tutorial: nil)
        #expect(!notSaved.draft.savePassword)
        #expect(notSaved.draft.password.isEmpty)
        #expect(services.connected.isEmpty)
        #expect(services.savedPasswords.isEmpty)
        services.storedPasswords.insert(id)
        let saved = AddMachineFlow.editing(record, store: store, services: services, tutorial: nil)
        #expect(saved.draft.savePassword)
        #expect(saved.draft.password.isEmpty)
        #expect(services.connected.isEmpty)
    }
}
