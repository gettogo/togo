import SwiftUI

enum ChatFramePart: Hashable {
    case row(String)
    case leaf(String, Int)
    case working
    case composer
}

extension View {
    func chatFrameMarker(_ part: ChatFramePart) -> some View {
        #if DEBUG
        modifier(ChatFrameMarker(part: part))
        #else
        self
        #endif
    }
}

#if DEBUG
import UIKit

extension EnvironmentValues {
    @Entry var measuresChatFrames = false
}

private struct ChatFrameMarker: ViewModifier {
    @Environment(\.measuresChatFrames) private var enabled
    let part: ChatFramePart

    func body(content: Content) -> some View {
        if enabled {
            content.background { ChatFrameGuide(part: part).allowsHitTesting(false).accessibilityHidden(true) }
        } else {
            content
        }
    }
}

private struct ChatFrameGuide: UIViewRepresentable {
    let part: ChatFramePart

    func makeUIView(context: Context) -> ChatFrameGuideView {
        let view = ChatFrameGuideView()
        view.isUserInteractionEnabled = false
        view.isAccessibilityElement = false
        return view
    }

    func updateUIView(_ view: ChatFrameGuideView, context: Context) { view.part = part }
}

/// Carries a stable identifier with the view's geometry, including presentation-layer movement.
/// It exists only in opted-in Debug hosts and draws nothing.
final class ChatFrameGuideView: UIView {
    var part: ChatFramePart?
}
#endif
