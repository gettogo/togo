import SwiftUI
import TogoKit

struct AboutView: View {
    static let sourceURL = URL(string: "https://github.com/gettogo/togo")!
    static let privacyURL = URL(string: "https://togoapp.dev/privacy/")!
    static let supportURL = URL(string: "https://togoapp.dev/support/")!

    let appVersion: String
    let settings: AppSettings
    let machines: [SettingsMachine]
    #if DEBUG
    /// The debug screens, in debug builds only.
    @State private var developerShown = false
    #endif

    var body: some View {
        List {
            Section {
                // Values in mute: the system's secondary gray falls under 4.5:1 on the light rows.
                LabeledContent {
                    Text(verbatim: appVersion).foregroundStyle(Color.togoMute)
                } label: {
                    Text(settingsText("settings.about.version", "Version"))
                }
                Link(destination: Self.sourceURL) {
                    LabeledContent(settingsText("settings.about.source", "Source code")) {
                        HStack(spacing: 6) {
                            Text(verbatim: "github.com/gettogo/togo")
                                .foregroundStyle(Color.togoMute)
                            Image(systemName: "arrow.up.forward")
                                .font(.footnote.weight(.semibold))
                                .accessibilityHidden(true)
                        }
                    }
                }
                .frame(minHeight: 44)
            }
            .groupedRows()
            if !machines.isEmpty {
                Section {
                    ForEach(machines) { machine in
                        LabeledContent {
                            Text(verbatim: helperVersion(machine.helper)).foregroundStyle(Color.togoMute)
                        } label: {
                            Text(verbatim: machine.name)
                        }
                    }
                } header: {
                    heading(settingsText("settings.about.helpers", "Helpers"))
                }
                .groupedRows()
            }
            Section {
                NavigationLink {
                    ReportProblemView(settings: settings, appVersion: appVersion, machines: machines)
                } label: {
                    Text(settingsText("settings.about.report", "Report a problem"))
                }
                NavigationLink {
                    AcknowledgmentsView()
                } label: {
                    Text(settingsText("settings.about.acknowledgments", "Acknowledgments"))
                }
                webLink(settingsText("settings.about.support", "Support"), Self.supportURL)
                webLink(settingsText("settings.about.privacy", "Privacy policy"), Self.privacyURL)
            } footer: {
                SectionFooter {
                    Text(settingsText(
                        "settings.about.licenses.footer",
                        "Togo is free software under the GNU GPL version 3. The helper and relay are MIT licensed."
                    ))
                }
            }
            .groupedRows()
            #if DEBUG
            Section {
                Button { developerShown = true } label: {
                    Label { Text(verbatim: "Developer") } icon: { Image(systemName: "ladybug") }
                }
                .frame(minHeight: 44)
            }
            .groupedRows()
            #endif
        }
        .groupedList()
        #if DEBUG
        .presentCover("settings.developer", isPresented: $developerShown, nests: true) { DebugEntry.root() }
        #endif
        .navigationTitle(settingsText("settings.about.title", "About"))
        .navigationBarTitleDisplayMode(.inline)
    }

    /// A row that opens a page on the website.
    private func webLink(_ title: String, _ url: URL) -> some View {
        Link(destination: url) {
            LabeledContent(title) {
                Image(systemName: "arrow.up.forward")
                    .font(.footnote.weight(.semibold))
                    .accessibilityHidden(true)
            }
        }
        .frame(minHeight: 44)
    }

    private func heading(_ text: String) -> some View {
        Text(text)
            .font(.headline)
            .foregroundStyle(Color.togoSnow)
            .textCase(nil)
    }

    private func helperVersion(_ helper: HelperState) -> String {
        switch helper {
        case .checking: settingsText("settings.about.helper.checking", "Checking")
        case .present(let version, _): version
        case .missing: settingsText("settings.about.helper.missing", "Not installed")
        case .outdated(let found) where found.isEmpty: settingsText("settings.about.helper.outdatedUnknown", "Update available")
        case .outdated(let found): settingsText("settings.about.helper.outdated", "\(found), update available")
        case .newer: settingsText("settings.about.helper.newer", "Newer than this app")
        }
    }
}
