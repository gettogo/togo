import SwiftUI

/// A component Togo ships, with its full license text bundled as `ack-<id>.txt`. For a Swift
/// package `id` is its identity in Package.resolved, which the acknowledgments test checks.
struct Acknowledgment: Identifiable, Equatable, Sendable {
    var id: String
    var name: String
    var license: String
    /// Vendored with changes; Apache 2.0 section 4(b) asks for that to be stated.
    var modified = false

    var text: String? {
        guard let url = Bundle.main.url(forResource: "ack-\(id)", withExtension: "txt") else { return nil }
        return try? String(contentsOf: url, encoding: .utf8)
    }

    static let gpl = "GNU GPL v3"
    static let apache = "Apache 2.0"

    static let all: [Acknowledgment] = [
        Acknowledgment(id: "togo", name: "Togo", license: gpl),
        Acknowledgment(id: "mosh", name: "Mosh", license: gpl),
        Acknowledgment(id: "togo-helper", name: "Togo helper and relay", license: "MIT"),
        Acknowledgment(id: "bigint", name: "BigInt", license: "MIT"),
        Acknowledgment(id: "citadel", name: "Citadel", license: "MIT", modified: true),
        Acknowledgment(id: "protobuf", name: "Protocol Buffers", license: "BSD 3-Clause"),
        Acknowledgment(id: "swift-asn1", name: "swift-asn1", license: apache),
        Acknowledgment(id: "swift-atomics", name: "swift-atomics", license: apache),
        Acknowledgment(id: "swift-collections", name: "swift-collections", license: apache),
        Acknowledgment(id: "swift-crypto", name: "swift-crypto", license: apache),
        Acknowledgment(id: "swift-log", name: "swift-log", license: apache),
        Acknowledgment(id: "swift-nio", name: "swift-nio", license: apache),
        Acknowledgment(id: "swift-nio-ssh", name: "swift-nio-ssh", license: apache, modified: true),
        Acknowledgment(id: "swiftterm", name: "SwiftTerm", license: "MIT"),
        Acknowledgment(id: "argmax-oss-swift", name: "WhisperKit", license: "MIT"),
        Acknowledgment(id: "swift-transformers", name: "swift-transformers, in WhisperKit", license: apache),
        Acknowledgment(id: "agent-marks", name: "Agent marks", license: "CC0 1.0 and MIT"),
    ]
}

struct AcknowledgmentsView: View {
    var body: some View {
        List {
            Section {
                ForEach(Acknowledgment.all) { item in
                    NavigationLink {
                        AcknowledgmentTextView(item: item)
                    } label: {
                        LabeledContent {
                            Text(item.license)
                        } label: {
                            Text(item.name)
                            if item.modified {
                                Text(settingsText("settings.ack.modified", "Modified"))
                            }
                        }
                    }
                }
            } footer: {
                Text(settingsText("settings.ack.footer", "Togo is built with this software. Tap one to read its license."))
            }
            .groupedRows()
        }
        .groupedList()
        .navigationTitle(settingsText("settings.ack.title", "Acknowledgments"))
        .navigationBarTitleDisplayMode(.inline)
    }
}

private struct AcknowledgmentTextView: View {
    let item: Acknowledgment

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                if item.modified {
                    Text(settingsText(
                        "settings.ack.modified.notice",
                        "Togo ships a modified copy of \(item.name). Each changed source file starts with a line describing the change. The modified source is at github.com/gettogo/togo."
                    ))
                    .font(.body.weight(.semibold))
                }
                Text(item.text ?? settingsText("settings.ack.missing", "The license text is missing from this build."))
                    .textSelection(.enabled)
            }
            .foregroundStyle(Color.togoSnow)
            .padding(20)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .background(Color.togoInk)
        .navigationTitle(item.name)
        .navigationBarTitleDisplayMode(.inline)
    }
}
