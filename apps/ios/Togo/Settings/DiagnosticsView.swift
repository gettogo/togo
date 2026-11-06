import SwiftUI
import TogoKit

/// What the report holds right now, for the page to show and share.
struct DiagnosticsSummary: Equatable {
    var events: Int
    var machines: Int
    /// The report as plain text, for the preview.
    var text: String
    /// The JSON report written to a file named for today, for sharing.
    var file: URL?
}

/// What the app knows about its own health, for reporting a problem. The report is kept on
/// this iPhone and goes nowhere unless the person shares it.
struct DiagnosticsView: View {
    let settings: AppSettings
    var recorder: DiagnosticsRecorder = .shared
    @State private var summary: DiagnosticsSummary?

    var body: some View {
        DiagnosticsPage(summary: summary, clear: clear)
            .task { await load() }
    }

    private func load() async {
        summary = await Self.summary(recorder: recorder, settings: settings)
    }

    /// The report as it stands, with its file written for sharing.
    static func summary(recorder: DiagnosticsRecorder, settings: AppSettings) async -> DiagnosticsSummary {
        let report = await recorder.report(settings: settings)
        return DiagnosticsSummary(
            events: report.events.count, machines: report.machines.count, text: report.text(), file: write(report)
        )
    }

    private func clear() {
        Task {
            await recorder.clear()
            Self.removeExports()
            await load()
        }
    }

    /// The name people see when they save or send the report.
    static func fileName(on date: Date = Date()) -> String {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.dateFormat = "yyyy-MM-dd"
        return "togo-diagnostics-\(formatter.string(from: date)).json"
    }

    private static var exportFolder: URL {
        FileManager.default.temporaryDirectory.appendingPathComponent("diagnostics-export", isDirectory: true)
    }

    /// Earlier exports leave with Clear, and a new one replaces them.
    private static func removeExports() {
        try? FileManager.default.removeItem(at: exportFolder)
    }

    private static func write(_ report: DiagnosticsReport) -> URL? {
        guard let data = try? report.json() else { return nil }
        removeExports()
        let folder = exportFolder
        try? FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        let url = folder.appendingPathComponent(fileName())
        do {
            try data.write(to: url, options: [.atomic, .completeFileProtection])
            return url
        } catch {
            return nil
        }
    }
}

struct DiagnosticsPage: View {
    let summary: DiagnosticsSummary?
    let clear: () -> Void
    @State private var previewing: Bool
    @State private var confirmingClear = false

    init(summary: DiagnosticsSummary?, clear: @escaping () -> Void, previewing: Bool = false) {
        self.summary = summary
        self.clear = clear
        _previewing = State(initialValue: previewing)
    }

    var body: some View {
        List {
            Section {
                LabeledContent(
                    settingsText("diagnostics.events", "Events recorded"), value: (summary?.events ?? 0).formatted()
                )
                .frame(minHeight: 44)
                LabeledContent(
                    settingsText("diagnostics.machines", "Machines"), value: (summary?.machines ?? 0).formatted()
                )
                .frame(minHeight: 44)
            } footer: {
                Text(settingsText(
                    "diagnostics.included",
                    "Includes app and iOS versions, device model, a settings summary and recent connection events. Machines show as codes. Never includes names, addresses, folders, session names, prompts, terminal text, keys or passwords."
                ))
            }
            .groupedRows()
            Section {
                Button {
                    previewing = true
                } label: {
                    Label(settingsText("diagnostics.preview", "Preview"), systemImage: "doc.text.magnifyingglass")
                }
                .frame(minHeight: 44)
                .disabled(summary == nil)
                if let file = summary?.file {
                    ShareLink(item: file) {
                        Label(settingsText("diagnostics.share", "Share report"), systemImage: "square.and.arrow.up")
                    }
                    .frame(minHeight: 44)
                }
            } footer: {
                Text(settingsText("diagnostics.sent", "Togo never sends this report. It leaves this device only when you share it."))
            }
            .groupedRows()
            Section {
                Button(role: .destructive) {
                    confirmingClear = true
                } label: {
                    Label(settingsText("diagnostics.clear", "Clear"), systemImage: "trash")
                }
                .frame(minHeight: 44)
                .disabled((summary?.events ?? 0) == 0)
            }
            .groupedRows()
        }
        .groupedList()
        .navigationTitle(settingsText("settings.diagnostics.title", "Diagnostics"))
        .navigationBarTitleDisplayMode(.inline)
        .confirmationDialog(
            settingsText("diagnostics.clear.title", "Clear recorded events?"), isPresented: $confirmingClear, titleVisibility: .visible
        ) {
            Button(settingsText("diagnostics.clear", "Clear"), role: .destructive, action: clear)
        }
        .sheet(isPresented: $previewing) {
            DiagnosticsPreview(text: summary?.text ?? "")
        }
    }
}

/// The report as it would be sent, as text.
struct DiagnosticsPreview: View {
    let text: String
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            ScrollView {
                Text(verbatim: text)
                    .font(.system(.footnote, design: .monospaced))
                    .textSelection(.enabled)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(Layout.margin)
            }
            .background(Color.togoInk)
            .navigationTitle(settingsText("diagnostics.preview", "Preview"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button(settingsText("diagnostics.done", "Done")) { dismiss() }
                }
            }
        }
        .togoControls()
    }
}
