import Foundation
import Testing
import TogoKit
@testable import Togo

/// Machines from iCloud on a device that has no key for them read as "set up here", not as a
/// sign-in that failed, and connect with this device's own key once it is on them.
@MainActor struct DeviceMachinesTests {
    private let other = "key-made-on-the-other-device"
    private let mine = "key-made-on-this-device"

    private func record(_ auth: AuthMethodRef, id: HostID = Samples.studio) -> HostRecord {
        HostRecord(
            id: id,
            profile: HostProfile(id: id, name: "Studio", endpoint: HostEndpoint(host: "studio.local", port: 22, user: "sam"), auth: auth),
            symbol: "laptop"
        )
    }

    private func make(
        _ backend: MemoryBackend = MemoryBackend(), here: Set<String>, deviceKey: String? = nil
    ) -> DeviceMachines {
        DeviceMachines(backend: backend, hasKey: { here.contains($0) }, deviceKeyID: { deviceKey })
    }

    // MARK: Which state

    @Test func aSyncedMachineWithNoKeyHereNeedsSetUp() {
        let machines = make(here: [])
        _ = machines.resolve([record(.key(id: other))])
        #expect(machines.needsSetUp(Samples.studio, reach: .keyMissing))
        // Refused with this device's key, or with an unknown host key the monitor cannot ask about.
        #expect(machines.needsSetUp(Samples.studio, reach: .authNeeded))
    }

    @Test func aMachineSignedInHereThatRefusesNeedsSignInAgain() {
        let machines = make(here: [other])
        _ = machines.resolve([record(.key(id: other))])
        machines.signedIn(to: Samples.studio)
        #expect(!machines.needsSetUp(Samples.studio, reach: .authNeeded))
    }

    @Test func aMachineAddedWithThisDevicesKeyNeedsSignInAgain() {
        #expect(!DeviceMachines.needsSetUp(reach: .authNeeded, auth: .key(id: mine), signedInHere: false, deviceKeyID: mine))
    }

    @Test func aPasswordMachineNeverNeedsSetUp() {
        #expect(!DeviceMachines.needsSetUp(reach: .authNeeded, auth: .password, signedInHere: false, deviceKeyID: nil))
    }

    @Test func onlyAMissingKeyOrRefusedSignInNeedsSetUp() {
        let others: [HostSnapshot.Reach] = [.connecting, .waking, .online, .offline("x"), .failed("x"), .hostKeyChanged, .deviceOffline]
        for reach in others {
            #expect(!DeviceMachines.needsSetUp(reach: reach, auth: .key(id: other), signedInHere: false, deviceKeyID: nil))
        }
    }

    @Test func aMachineSetUpHereNoLongerNeedsIt() {
        let machines = make(here: [mine], deviceKey: mine)
        _ = machines.resolve([record(.key(id: other))])
        machines.setUp(Samples.studio, keyID: mine)
        #expect(!machines.needsSetUp(Samples.studio, reach: .authNeeded))
    }

    // MARK: Which key

    @Test func aMissingKeyFallsBackToThisDevicesKey() {
        let machines = make(here: [mine], deviceKey: mine)
        #expect(machines.resolve([record(.key(id: other))]).first?.profile.auth == .key(id: mine))
    }

    @Test func aKeyThatIsHereIsKept() {
        let machines = make(here: [other, mine], deviceKey: mine)
        #expect(machines.resolve([record(.key(id: other))]).first?.profile.auth == .key(id: other))
    }

    @Test func withNoKeyHereTheRecordIsLeftAlone() {
        let machines = make(here: [])
        #expect(machines.resolve([record(.key(id: other))]).first?.profile.auth == .key(id: other))
        #expect(machines.resolve([record(.password)]).first?.profile.auth == .password)
    }

    /// The key chosen while setting up here wins, even over a key the record names that is here.
    @Test func theKeySetUpHereWins() {
        let machines = make(here: [other, mine], deviceKey: mine)
        machines.setUp(Samples.studio, keyID: mine)
        #expect(machines.resolve([record(.key(id: other))]).first?.profile.auth == .key(id: mine))
    }

    /// Saving keeps the synced record's own key, so the other devices keep signing in with theirs.
    @Test func savingSendsTheSyncedKeyNotThisDevicesKey() {
        let machines = make(here: [mine], deviceKey: mine)
        let resolved = machines.resolve([record(.key(id: other))])
        #expect(machines.syncedVersion(of: resolved[0]).profile.auth == .key(id: other))
    }

    /// Editing a machine set up here, such as renaming it, starts from the record with this
    /// device's key in it. The saved record keeps the key it named for the other devices.
    @Test func anEditLeavingTheSignInAloneKeepsTheSyncedKey() {
        let machines = make(here: [mine], deviceKey: mine)
        machines.setUp(Samples.studio, keyID: mine)
        var edited = machines.resolve([record(.key(id: other))])[0]
        #expect(edited.profile.auth == .key(id: mine))
        edited.profile.name = "Studio Mac"
        let saved = machines.editedVersion(of: edited)
        #expect(saved.profile.auth == .key(id: other))
        #expect(saved.profile.name == "Studio Mac")
    }

    @Test func anEditThatChangesTheSignInSavesIt() {
        let machines = make(here: [mine], deviceKey: mine)
        var edited = machines.resolve([record(.key(id: other))])[0]
        edited.profile.auth = .password
        #expect(machines.editedVersion(of: edited).profile.auth == .password)
    }

    // MARK: Kept on this device

    @Test func whatThisDeviceLearnedSurvivesARelaunch() {
        let backend = MemoryBackend()
        let first = make(backend, here: [mine], deviceKey: mine)
        first.setUp(Samples.studio, keyID: mine)
        first.signedIn(to: Samples.build)

        let relaunched = make(backend, here: [mine], deviceKey: mine)
        let resolved = relaunched.resolve([record(.key(id: other)), record(.key(id: other), id: Samples.build)])
        #expect(resolved.first?.profile.auth == .key(id: mine))
        #expect(!relaunched.needsSetUp(Samples.studio, reach: .authNeeded))
        #expect(!relaunched.needsSetUp(Samples.build, reach: .authNeeded))
    }

    @Test func removingAMachineForgetsIt() {
        let backend = MemoryBackend()
        let first = make(backend, here: [mine], deviceKey: nil)
        first.setUp(Samples.studio, keyID: mine)
        first.forget(Samples.studio)

        let relaunched = make(backend, here: [mine], deviceKey: nil)
        _ = relaunched.resolve([record(.key(id: other))])
        #expect(relaunched.needsSetUp(Samples.studio, reach: .keyMissing))
    }
}
