import SwiftUI

/// A session's bib number, the short id used on tabs, notifications and `togo attach`.
struct BibText: View {
    let bib: String
    var style: Font.TextStyle = .subheadline

    init(_ bib: String, style: Font.TextStyle = .subheadline) {
        self.bib = bib
        self.style = style
    }

    var body: some View {
        Text(verbatim: bib)
            .font(.system(style, design: .monospaced))
            .monospacedDigit()
            .foregroundStyle(Color.togoMute)
            // trunc-ok: a bib is two or three digits
            .lineLimit(1)
            .fixedSize()
    }
}
