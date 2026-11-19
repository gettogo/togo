import XCTest

@MainActor final class StrictReplayUITests: XCTestCase {
    override func setUp() async throws {
        continueAfterFailure = false
        XCUIDevice.shared.orientation = .portrait
    }

    func testStrictLaunchOpensTheRecordedChat() throws {
        let app = launchTogo(arguments: ["-ui-one-machine", "-ui-still-loops", "-ui-strict-replay", "strict-send-reopen"])
        let probe = app.descendants(matching: .any)["replay.strict"]
        XCTAssertTrue(probe.waitForExistence(timeout: 5), "Strict replay must expose its verdict")
        XCTAssertEqual(probe.value as? String, "pending")
        app.open(try XCTUnwrap(URL(string: "togo://session/01")))
        let field = app.descendants(matching: .any)["composer.field"].firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 10), "Strict replay must open the recorded chat; verdict: \(probe.value ?? "none")")
        XCTAssertEqual(probe.value as? String, "pending", "The tape must still expect the user's sends")
    }

    func testMissingStrictFixtureIsVisible() {
        let app = launchTogo(arguments: ["-ui-one-machine", "-ui-strict-replay", "absent-tape"])
        let probe = app.descendants(matching: .any)["replay.strict"]
        XCTAssertTrue(probe.waitForExistence(timeout: 5))
        XCTAssertEqual(probe.value as? String, "fixture unavailable")
    }

}
