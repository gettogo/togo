import Foundation
import SwiftUI
import Testing
import TogoKit
import TogoTerminal
@testable import Togo

@MainActor struct SettingsTests {
    @Test(arguments: [false, true])
    func optionKeyChoiceReachesTheTerminal(meta: Bool) throws {
        let data = Data("{\"optionAsMeta\":\(meta)}".utf8)
        let settings = try JSONDecoder().decode(AppSettings.self, from: data)
        #expect(LiveTerminal.terminalSettings(settings).optionAsMeta == meta)
        let saved = try #require(JSONSerialization.jsonObject(with: JSONEncoder().encode(settings)) as? [String: Any])
        #expect(saved["optionAsMeta"] as? Bool == meta)
    }

    /// Turning command text off or on reaches a machine through its relay; until the machine
    /// confirms over SSH, the page says the change is on its way.
    @Test func commandChoicePendingUntilTheMachineConfirms() {
        let sections = [
            MachineSection(
                id: Samples.studio, name: "Studio", symbol: .mini,
                reachability: .offline(since: Samples.now, reason: "asleep"), helper: .present(version: "1", live: true), sessions: []
            ),
            MachineSection(
                id: Samples.build, name: "Build box", symbol: .mini, reachability: .online,
                helper: .present(version: "1", live: true), sessions: []
            ),
        ]
        var machines = NotificationValues.machines(sections, paired: [Samples.studio], commandsOn: [Samples.studio])
        #expect(machines.map(\NotificationMachine.helperCommand) == [true, nil])
        #expect(NotificationMachinesSection.commandNote(machines[0], showing: false) == "Turns off when Studio receives it")
        #expect(NotificationMachinesSection.commandNote(machines[0], showing: true) == nil)
        #expect(NotificationMachinesSection.commandNote(machines[1], showing: true) == nil, "pairs with the choice")
        machines[0].helperCommand = false
        #expect(NotificationMachinesSection.commandNote(machines[0], showing: true) == "Turns on when Studio receives it")
    }

    @Test func settingsWithoutNewerFieldsDecodeWithDefaults() throws {
        let saved = Data("""
        {"themeID":"plain","fontName":"Menlo","fontSize":15,"cursor":"bar",
         "syncEnabled":false,"resumeLock":true,"transport":"auto"}
        """.utf8)
        let settings = try JSONDecoder().decode(AppSettings.self, from: saved)
        #expect(settings.themeID == "plain")
        #expect(settings.fontSize == 15)
        // A record never decides sync on this device, even one that says off; `SyncSwitch` does.
        #expect(settings.syncEnabled)
        #expect(settings.terminalKeys == TerminalKeysConfig())
        #expect(settings.gestures == GestureBindings())
        #expect(settings.customThemes.isEmpty)
        #expect(settings.readNewOutput)
    }

    /// The default and every id a legacy theme migrates to is a choice the list offers, so a
    /// stored theme from before the rename still selects the same theme, not the first one.
    @Test func migratedThemeIDsAreOfferedChoices() {
        let offered = Set(ThemeChoice.all(imported: []).map(\.id))
        #expect(ThemeChoice.all(imported: []).first?.id == ThemeChoice.defaultID)
        #expect(AppSettings().themeID == ThemeChoice.defaultID)
        for old in ["", "-dark", "-light"].map({ "lan" + "tern" + $0 }) + ["default"] {
            #expect(offered.contains(AppSettings.migratedThemeID(old)))
        }
        #expect(offered.contains(ThemeChoice.systemID))
    }

    /// Only "Match System Appearance" changes with the appearance; the default stays Togo Dark
    /// in light mode, where programs that hard-code dark-friendly colours still read.
    @Test func onlyTheSystemChoiceFollowsTheAppearance() throws {
        let choices = ThemeChoice.all(imported: [])
        for choice in choices where choice.id != ThemeChoice.systemID {
            #expect(choice.theme(for: .light) == choice.theme(for: .dark), "\(choice.id) changed with the appearance")
        }
        let system = try #require(choices.first { $0.id == ThemeChoice.systemID })
        #expect(system.theme(for: .dark).id == "togo-dark")
        #expect(system.theme(for: .light).id == "togo-light")
        #expect(ThemeChoice.current(AppSettings()).theme(for: .light).id == "togo-dark")
    }

    @Test func unknownThemeFallsBackToTogoDark() {
        var settings = AppSettings()
        settings.themeID = "gone"
        #expect(ThemeChoice.current(settings).id == ThemeChoice.defaultID)
    }

    /// Chrome over the terminal takes the theme's appearance, not the system's.
    @Test(arguments: [
        ("togo-dark", ColorScheme.light, ColorScheme.dark),
        ("togo-dark", ColorScheme.dark, ColorScheme.dark),
        ("togo-light", ColorScheme.dark, ColorScheme.light),
        ("plain", ColorScheme.light, ColorScheme.dark),
        ("system", ColorScheme.light, ColorScheme.light),
        ("system", ColorScheme.dark, ColorScheme.dark),
    ])
    func backdropFollowsTheTheme(_ id: String, _ system: ColorScheme, _ expected: ColorScheme) {
        var settings = AppSettings()
        settings.themeID = id
        let backdrop = TerminalBackdrop.resolve(settings, system: system)
        #expect(backdrop.scheme == expected)
        #expect(backdrop.background == Color(terminalHex: ThemeChoice.current(settings).theme(for: system).background))
    }

    /// Leaving the terminal hands the bar an explicit scheme, never nil, which left it dark.
    @Test func theBarFollowsTheLayerBackToTheSystem() {
        var settings = AppSettings()
        settings.themeID = "togo-dark"
        let terminal = TerminalBackdrop.resolve(settings, system: .light)
        #expect(TerminalBackdrop.barScheme(terminal, system: .light) == .dark)
        #expect(TerminalBackdrop.barScheme(nil, system: .light) == .light)
        #expect(TerminalBackdrop.barScheme(nil, system: .dark) == .dark)
    }

    @Test func settingsDropOnlyTheBadImportedTheme() throws {
        var settings = AppSettings()
        let good = BuiltInThemes.all[0]
        var bad = good
        bad.id = "imported-broken"
        bad.ansi = ["#000000"]
        settings.customThemes = [good, bad]
        let decoded = try JSONDecoder().decode(AppSettings.self, from: try JSONEncoder().encode(settings))
        #expect(decoded.customThemes == [good])
    }

    @Test func settingsRoundTrip() throws {
        var settings = AppSettings()
        settings.terminalKeys.controller = false
        settings.gestures.touchMouse = true
        settings.readNewOutput = false
        let decoded = try JSONDecoder().decode(AppSettings.self, from: try JSONEncoder().encode(settings))
        #expect(decoded == settings)
    }

    @Test func themeImportRefusesFilesPastTheCap() throws {
        let directory = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let atCap = directory.appending(path: "at-cap")
        let pastCap = directory.appending(path: "past-cap")
        try Data(count: ThemeImport.maximumBytes).write(to: atCap)
        try Data(count: ThemeImport.maximumBytes + 1).write(to: pastCap)

        #expect(try ThemeImport.capped(FileHandle(forReadingFrom: atCap)).count == ThemeImport.maximumBytes)
        #expect(throws: ThemeImport.TooLarge.self) { try ThemeImport.capped(FileHandle(forReadingFrom: pastCap)) }
    }

    @Test func themeImportErrorsNameTheFile() {
        #expect(ThemeImport.message(for: ThemeImportError.missingColor("ansi 3"), filename: "Dracula.itermcolors")
            == "Couldn't import Dracula.itermcolors: it has no ANSI color 3.")
        #expect(ThemeImport.message(for: ThemeImport.TooLarge(), filename: "big")
            == "Couldn't import big: it's too large to be a theme.")
        #expect(ThemeImport.message(for: CocoaError(.fileReadNoPermission), filename: nil)
            == "Couldn't open that file. Choose an iTerm2, Ghostty or Xresources theme.")
    }

    /// Packages that resolve but never reach the app binary.
    private static let notLinked: Set<String> = [
        // SwiftTerm's command line tools.
        "swift-argument-parser",
        // SwiftTerm's image decoding for platforms without ImageIO.
        "swift-png", "h",
        // NIO and Crypto use it only on Linux and Windows.
        "swift-system",
        // The snapshot test library and its dependencies.
        "swift-snapshot-testing", "swift-custom-dump", "swift-issue-reporting", "swift-syntax",
    ]

    private static let repo = URL(filePath: #filePath)
        .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()

    @Test func acknowledgmentsCoverEveryLinkedPackage() throws {
        let manifests = [
            "packages/TogoKit/Package.resolved",
            "packages/TogoTerminal/Package.resolved",
            "packages/TogoMosh/Package.resolved",
            "apps/ios/Togo.xcodeproj/project.xcworkspace/xcshareddata/swiftpm/Package.resolved",
        ]
        var identities: Set<String> = []
        for path in manifests {
            let url = Self.repo.appending(path: path)
            guard FileManager.default.fileExists(atPath: url.path) else { continue }
            let resolved = try JSONDecoder().decode(Resolved.self, from: Data(contentsOf: url))
            identities.formUnion(resolved.pins.map(\.identity))
        }
        let vendored = try FileManager.default.contentsOfDirectory(atPath: Self.repo.appending(path: "vendor").path)
            .filter { !$0.hasPrefix(".") && !$0.hasSuffix(".md") }
            .map { $0.lowercased() }
        #expect(identities.contains("swift-nio"), "Package.resolved files were not found under \(Self.repo.path)")

        let listed = Set(Acknowledgment.all.map(\.id))
        #expect(listed.isDisjoint(with: Self.notLinked), "acknowledged but not linked: \(listed.intersection(Self.notLinked).sorted())")
        let missing = identities.subtracting(Self.notLinked).union(vendored).union(["mosh", "protobuf"]).subtracting(listed)
        #expect(missing.isEmpty, "not acknowledged: \(missing.sorted())")
        #expect(Set(Acknowledgment.all.filter(\.modified).map(\.id)) == Set(vendored))
    }

    @Test func everyAcknowledgmentHasItsLicenseText() {
        for item in Acknowledgment.all {
            let text = item.text ?? ""
            #expect(text.count > 500, "\(item.id) has no license text")
        }
        #expect(Acknowledgment.all.first { $0.id == "mosh" }?.text?.contains("GNU GENERAL PUBLIC LICENSE") == true)
        #expect(Acknowledgment.all.first { $0.id == "protobuf" }?.text?.contains("Redistributions in binary form") == true)
    }

    private struct Resolved: Decodable {
        struct Pin: Decodable { var identity: String }
        var pins: [Pin]
    }
}
