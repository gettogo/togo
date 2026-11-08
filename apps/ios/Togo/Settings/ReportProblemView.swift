import SwiftUI
import TogoKit

/// Where a person goes to tell the project about a problem: the diagnostics export and a
/// GitHub issue that already holds the non-personal facts.
struct ReportProblemView: View {
    let settings: AppSettings
    let appVersion: String
    let machines: [SettingsMachine]
    var recorder: DiagnosticsRecorder = .shared
    @State private var summary: DiagnosticsSummary?

    var body: some View {
        ReportProblemPage(file: summary?.file, issue: issue)
            .task { summary = await DiagnosticsView.summary(recorder: recorder, settings: settings) }
    }

    private var issue: URL {
        IssueLink.url(
            app: appVersion, os: UIDevice.current.systemVersion,
            // idiom-ok: the device family in a problem report; no layout depends on it.
            device: IssueLink.family(isPad: UIDevice.current.userInterfaceIdiom == .pad),
            helpers: machines.compactMap { Self.version(of: $0.helper) }
        )
    }

    static func version(of helper: HelperState) -> String? {
        switch helper {
        case .present(let version, _): version
        case .outdated(let found): found.isEmpty ? nil : found
        case .checking, .missing, .newer: nil
        }
    }
}

struct ReportProblemPage: View {
    /// The exported report, once it is written.
    let file: URL?
    let issue: URL

    var body: some View {
        List {
            Section {
                if let file {
                    ShareLink(item: file) {
                        Label(settingsText("report.export", "Export diagnostics"), systemImage: "square.and.arrow.up")
                    }
                    .frame(minHeight: 44)
                } else {
                    Label(settingsText("report.export", "Export diagnostics"), systemImage: "square.and.arrow.up")
                        .foregroundStyle(Color.togoMute)
                        .frame(minHeight: 44)
                }
            } header: {
                Text(settingsText("report.included", "The issue opens with the app version, the iOS version, whether this is an iPhone or an iPad, and the helper versions. It never holds machine names, addresses, user names, folders or session names. You can read and change it before you send it."))
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
                    .textCase(nil)
            } footer: {
                Text(settingsText("report.export.footer", "The report holds settings and recent connection events, with machines as codes. Togo never sends it."))
            }
            .groupedRows()
            Section {
                Link(destination: issue) {
                    LabeledContent {
                        Image(systemName: "arrow.up.forward")
                            .font(.footnote.weight(.semibold))
                            .accessibilityHidden(true)
                    } label: {
                        Text(settingsText("report.open", "Open an issue on GitHub"))
                    }
                }
                .frame(minHeight: 44)
            } footer: {
                Text(settingsText("report.attach", "Attach the exported file to the issue if you're comfortable sharing it."))
            }
            .groupedRows()
        }
        .groupedList()
        .navigationTitle(settingsText("report.title", "Report a problem"))
        .navigationBarTitleDisplayMode(.inline)
    }
}
