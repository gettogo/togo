import SwiftUI

struct AboutView: View {
    static let sourceURL = URL(string: "https://github.com/gettogo/togo")!

    let appVersion: String
    let machines: [SettingsMachine]

    var body: some View {
        List {
            Section {
                LabeledContent(settingsText("settings.about.version", "Version"), value: appVersion)
                Link(destination: Self.sourceURL) {
                    LabeledContent(settingsText("settings.about.source", "Source code")) {
                        HStack(spacing: 6) {
                            Text(verbatim: "github.com/gettogo/togo")
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
                        LabeledContent(machine.name, value: helperVersion(machine.helper))
                    }
                } header: {
                    heading(settingsText("settings.about.helpers", "Helpers"))
                }
                .groupedRows()
            }
            Section {
                NavigationLink {
                    AcknowledgmentsView()
                } label: {
                    Text(settingsText("settings.about.acknowledgments", "Acknowledgments"))
                }
            } footer: {
                Text(settingsText(
                    "settings.about.licenses.footer",
                    "Togo is free software under the GNU GPL version 3. The helper and relay are MIT licensed."
                ))
            }
            .groupedRows()
        }
        .groupedList()
        .navigationTitle(settingsText("settings.about.title", "About"))
        .navigationBarTitleDisplayMode(.inline)
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
        case .outdated(let found): settingsText("settings.about.helper.outdated", "\(found), update available")
        }
    }
}
