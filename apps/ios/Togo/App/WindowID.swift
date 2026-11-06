import CoreTransferable
import SwiftUI
import UniformTypeIdentifiers

/// One window of the app. Each has its own `AppStore`; the services and terminals are shared.
struct WindowID: Hashable, Sendable {
    let raw: UUID

    init() {
        raw = UUID()
    }

    /// The window of a view shown without a window root around it, as in previews and
    /// snapshot tests, where there is only ever one.
    static let single = WindowID()
}

extension EnvironmentValues {
    /// The window this view is in.
    @Entry var windowID = WindowID.single
}

extension UTType {
    /// A tab dragged between panes and windows, or handed to a new window.
    static let togoTab = UTType(exportedAs: "app.togo.tab")
}

extension TabID: Codable, Transferable {
    private enum CodingKeys: String, CodingKey {
        case hostID, sessionID
    }

    init(from decoder: any Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        self.init(
            hostID: try values.decode(HostID.self, forKey: .hostID),
            sessionID: try values.decode(String.self, forKey: .sessionID)
        )
    }

    func encode(to encoder: any Encoder) throws {
        var values = encoder.container(keyedBy: CodingKeys.self)
        try values.encode(hostID, forKey: .hostID)
        try values.encode(sessionID, forKey: .sessionID)
    }

    static var transferRepresentation: some TransferRepresentation {
        CodableRepresentation(contentType: .togoTab)
    }
}
