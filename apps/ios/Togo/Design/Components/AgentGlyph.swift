import SwiftUI
import TogoKit

/// An agent's mark in the current foreground style and font, the size of an SF Symbol beside
/// it. Logos are the agents' official marks kept as symbol assets, always in one colour; agents
/// without one Togo may show get one or two letters in SF Mono.
struct AgentGlyph: View {
    let agent: String

    var body: some View {
        Self.text(agent)
            .accessibilityHidden(true)
    }

    /// The mark as text, to sit inline in a line of text and follow its font.
    static func text(_ agent: String) -> Text {
        switch AgentCatalog.mark(agent) {
        case .logo(let name): Text(Image(name))
        case .symbol(let name): Text(Image(systemName: name))
        case .monogram(let letters): Text(verbatim: letters).monospaced().fontWeight(.semibold)
        }
    }
}

#if DEBUG
#Preview {
    VStack(alignment: .leading, spacing: 12) {
        ForEach(AgentCatalog.entries, id: \.id) { entry in
            HStack(spacing: 12) {
                AgentGlyph(agent: entry.id).font(.title3).frame(width: 30, alignment: .leading)
                Text(verbatim: entry.name)
            }
        }
    }
    .foregroundStyle(Color.togoSnow)
    .padding()
    .background(Color.togoInk)
}
#endif
