import Foundation
import Testing
import TogoPush

/// Each file in `protocol/fixtures/render` (other than the clean table) is a captured agent
/// screen carried through the helper's detector, approval store and sender to the plaintext a
/// device opens, with the notification text the helper expects for it.
struct RenderGoldenTests {
    struct Fixture: Decodable {
        struct Expect: Decodable {
            var title: String
            var subtitle: String
            var body: String
            var category: String
        }

        var name: String
        var plaintext: PushPayload
        var expect: Expect
    }

    static func fixtures() throws -> [Fixture] {
        var dir = URL(filePath: #filePath)
        for _ in 0..<5 { dir.deleteLastPathComponent() }
        dir.append(path: "protocol/fixtures/render")
        let files = try FileManager.default.contentsOfDirectory(at: dir, includingPropertiesForKeys: nil)
            .filter { $0.pathExtension == "json" && $0.lastPathComponent != "clean.json" }
            .sorted { $0.lastPathComponent < $1.lastPathComponent }
        return try files.map { try JSONDecoder().decode(Fixture.self, from: Data(contentsOf: $0)) }
    }

    @Test func renderGoldenFixtures() throws {
        let fixtures = try Self.fixtures()
        #expect(fixtures.count >= 8)
        for f in fixtures {
            let text = try #require(NotificationTextBuilder.build(f.plaintext), "\(f.name)")
            #expect(text.title == f.expect.title, "\(f.name)")
            #expect(text.subtitle == f.expect.subtitle, "\(f.name)")
            #expect(text.body == f.expect.body, "\(f.name)")
            #expect(text.category == f.expect.category, "\(f.name)")
            #expect(text.interruption == .timeSensitive, "\(f.name)")
        }
    }

    @Test func renderedTextCarriesNoScreenDecoration() throws {
        for f in try Self.fixtures() {
            let text = try #require(NotificationTextBuilder.build(f.plaintext))
            for s in [text.title, text.subtitle, text.body] {
                #expect(!s.unicodeScalars.contains { (0x2500...0x259F).contains($0.value) }, "\(f.name): \(s)")
                #expect(!["❯", "›", "▸", ">"].contains { s.hasPrefix($0) }, "\(f.name): \(s)")
            }
        }
    }
}
