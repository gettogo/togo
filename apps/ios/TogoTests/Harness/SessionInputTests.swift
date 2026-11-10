import Foundation
import Testing
import TogoKit
@testable import Togo

/// Which input a session shows: the chat's composer, the terminal itself, or, while it is not
/// known yet, neither (the loader). The prompt bar is never an input line under any of them.
@MainActor struct SessionInputTests {
    @Test func eachLayerHasOneInput() {
        #expect(SessionInput(.chat) == .composer)
        #expect(SessionInput(.terminal) == .terminal)
        #expect(SessionInput(.pending) == .none)
        #expect(SessionInput(.chat).hidesPromptBar)
        #expect(SessionInput(.pending).hidesPromptBar)
        #expect(!SessionInput(.terminal).hidesPromptBar)
    }

    @Test func noSessionHasAnInputLineInThePromptBar() {
        // The terminal takes the typing itself and the chat has its composer: with nothing
        // waiting, the bar is away whichever layer shows.
        for layer in [SessionLayer.chat, .terminal, .pending] {
            let mode = PromptFeed.mode(
                requests: [], current: .start, hasMachines: true,
                composerTyping: SessionInput(layer).hidesPromptBar
            )
            #expect(mode == .hidden)
        }
    }

    @Test func beforeTheMachinesAreReadASessionWaits() {
        #expect(SessionLayer.beforeStore(hostsLoaded: false, listed: false, serves: false, expectsChat: false) == .pending)
        #expect(SessionLayer.beforeStore(hostsLoaded: true, listed: false, serves: false, expectsChat: true) == .terminal)
        #expect(SessionLayer.beforeStore(hostsLoaded: true, listed: true, serves: true, expectsChat: true) == .pending)
        #expect(SessionLayer.beforeStore(hostsLoaded: true, listed: true, serves: false, expectsChat: true) == .terminal)
        #expect(SessionLayer.beforeStore(hostsLoaded: true, listed: true, serves: true, expectsChat: false) == .terminal)
    }

    @Test func theLoaderGivesWayToTheTerminalIfDescribeStalls() async throws {
        let subject = HarnessSubject(hostID: UUID(), sessionID: "s_wait", capabilities: [Capability.harness])
        let store = HarnessSessionStore(
            subject: subject, services: FakeHarnessServices(descriptor: ChatTests.claude),
            expectsChat: true, pendingTimeout: .milliseconds(20)
        )
        #expect(store.shownLayer == .pending)
        store.startWaitClock()
        let deadline = ContinuousClock.now + .seconds(2)
        while store.shownLayer == .pending, ContinuousClock.now < deadline {
            try await Task.sleep(for: .milliseconds(5))
        }
        #expect(store.shownLayer == .terminal)
    }

    @Test func theLaunchHandsOverOnceTheSessionKnowsItsLayer() {
        #expect(!LaunchReadiness.ready(hostsLoaded: false, onScreen: nil))
        #expect(LaunchReadiness.ready(hostsLoaded: true, onScreen: nil))
        #expect(!LaunchReadiness.ready(hostsLoaded: true, onScreen: .pending))
        #expect(LaunchReadiness.ready(hostsLoaded: true, onScreen: .chat))
        #expect(LaunchReadiness.ready(hostsLoaded: true, onScreen: .terminal))
    }

    @Test func theAgentIsNamedAsEverywhereElse() {
        let omp = HarnessDescriptor.Adapter(id: "omp", name: "omp", harnessVersion: "1", revision: 1)
        #expect(HarnessSessionStore.displayName(omp) == "OMP")
        #expect(ComposerCopy.message(HarnessSessionStore.displayName(omp)) == "Message OMP")
        let other = HarnessDescriptor.Adapter(id: "newcomer", name: "Newcomer", harnessVersion: "1", revision: 1)
        #expect(HarnessSessionStore.displayName(other) == "Newcomer")
        #expect(HarnessSessionStore.displayName(nil) == "")
    }

    /// The chat's text column is the same width wherever it sits, so its edges never depend on
    /// what a row measured: `inset` in on a phone, centred at the column's widest when wide.
    @Test func theChatColumnOwnsItsWidth() {
        #expect(ChatLayout.textWidth(in: 402) == 402 - 2 * ChatLayout.inset)
        #expect(ChatLayout.textWidth(in: 1180) == ChatLayout.composerWidth)
        #expect(ChatLayout.textWidth(in: 20) == 0)
    }
}
