import SnapshotTesting
import SwiftUI
import Testing
import TogoTerminal
@testable import Togo

/// The prompt bar in each of its modes, at iPhone width and in a 500 pt column, light and dark.
@MainActor
struct PromptBarSnapshotTests {
    private static let request = PromptRequest(
        id: "r-04", sessionID: "s-04", bib: "04", sessionName: "app", agentName: "Claude Code",
        machineName: "Studio", question: "Run npm test?", command: "npm test -- --watch=false"
    )

    private static let longRequest = PromptRequest(
        id: "r-02", sessionID: "s-02", bib: "02", sessionName: "web-forms", agentName: "Codex",
        machineName: "MacBook Pro", question: "Run this migration?",
        command: "psql \"$DATABASE_URL\" -v ON_ERROR_STOP=1 -c \"BEGIN; ALTER TABLE accounts ADD COLUMN "
            + "last_seen_at timestamptz; UPDATE accounts SET last_seen_at = updated_at WHERE last_seen_at IS NULL; "
            + "CREATE INDEX CONCURRENTLY accounts_last_seen_idx ON accounts (last_seen_at); COMMIT;\" && "
            + "npm run db:verify -- --strict --report=./reports/migration.json"
    )

    private static let suggestions = [
        StartSuggestion(id: "1", agentName: "Claude Code", folder: "~/src/api", machineName: "MacBook Pro"),
        StartSuggestion(id: "2", agentName: "Codex", folder: "~/src/web", machineName: "MacBook Pro"),
        StartSuggestion(id: "3", agentName: "OpenCode", folder: "~/infra", machineName: "Studio"),
    ]

    /// The bar where its owner puts it: at the foot of the screen, over the ink.
    private func screen(_ model: PromptBarModel, keyboard: Bool = false) -> some View {
        VStack {
            Spacer()
            PromptBar(model: model)
                .frame(maxWidth: 500)
                .padding(.horizontal, 12)
                .padding(.bottom, 14)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .environment(\.hardwareKeyboard, KeyboardPresence(isConnected: { keyboard }))
    }

    private static let sizes: [ScreenSize] = [.iPhone17, .iPadHalf]

    @Test func request() {
        assertScreen(named: "prompt-request", sizes: Self.sizes) {
            screen(PromptBarModel(mode: .request(Self.request, position: 1, count: 3)))
        }
    }

    @Test func requestWithLongCommand() {
        assertScreen(named: "prompt-request-long", sizes: Self.sizes) {
            screen(PromptBarModel(mode: .request(Self.longRequest, position: 1, count: 1)))
        }
    }

    @Test func replying() {
        assertScreen(named: "prompt-replying", sizes: Self.sizes) {
            let model = PromptBarModel(mode: .request(Self.request, position: 1, count: 3))
            model.beginReply()
            model.replyDraft = "Only run the date tests first"
            return screen(model)
        }
    }

    @Test func start() {
        assertScreen(named: "prompt-start", sizes: Self.sizes) {
            screen(PromptBarModel(mode: .start))
        }
    }

    @Test func startWithKeyboard() {
        assertScreen(named: "prompt-start-keyboard", sizes: Self.sizes) {
            screen(PromptBarModel(mode: .start), keyboard: true)
        }
    }

    @Test func startOpen() {
        assertScreen(named: "prompt-start-open", sizes: Self.sizes) {
            let model = PromptBarModel(mode: .start)
            model.startSuggestions = Self.suggestions
            model.openStart()
            return screen(model)
        }
    }

    @Test func hidden() {
        assertScreen(named: "prompt-hidden") {
            screen(PromptBarModel(mode: .hidden))
        }
    }
}
