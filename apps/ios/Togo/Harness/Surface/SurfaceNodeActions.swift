import SwiftUI
import TogoKit
import UIKit

/// A producer's pointer actions stay reachable through the system touch menu.
struct SurfaceNodeActions: View {
    @Environment(\.openURL) private var openURL
    let node: SurfaceNode
    let context: SurfaceContext

    var body: some View {
        let actions = SurfaceValues.object(node.properties["actions"])
        let hold = SurfaceValues.array(actions["hold"])
        ForEach(hold.isEmpty ? SurfaceValues.array(actions["menu"]) : hold, id: \.self) { value in
            if value == .string("-") { Divider() }
            else if let header = SurfaceValues.string(SurfaceValues.object(value)["header"]) {
                Text(verbatim: header)
            } else {
                let properties = SurfaceValues.object(value)
                if let action = SurfaceValues.string(value) ?? SurfaceValues.string(properties["id"]) {
                    Button(role: properties["danger"] == .bool(true) ? .destructive : nil) {
                        perform(action)
                    } label: {
                        Text(verbatim: SurfaceValues.string(properties["label"]) ?? action.replacingOccurrences(of: ".", with: " ").capitalized)
                    }
                    .disabled(!context.session.canInteract || SurfaceValues.disabled(properties["disabled"]))
                }
            }
        }
    }

    private func perform(_ action: String) {
        switch action {
        case "copy": UIPasteboard.general.string = SurfaceValues.text(node.properties["text"] ?? node.properties["spans"] ?? node.properties["label"])
        case "open":
            if let href = SurfaceValues.string(node.properties["href"]), let url = URL(string: href) { openURL(url) }
        default: context.send(node, "action", ["act": .string(action)])
        }
    }
}
