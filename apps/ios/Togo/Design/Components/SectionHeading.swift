import SwiftUI

/// A plain title-case heading over a group of rows. Groups are separated by space, not by
/// small uppercase labels.
struct SectionHeading: View {
    let title: String
    var detail: String?

    init(_ title: String, detail: String? = nil) {
        self.title = title
        self.detail = detail
    }

    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                titleText
                Spacer(minLength: 8)
                detailText
            }
            VStack(alignment: .leading, spacing: 2) {
                titleText
                detailText
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
    }

    private var titleText: some View {
        Text(title)
            .font(.headline)
            .textCase(nil)
            .foregroundStyle(Color.togoSnow)
    }

    @ViewBuilder private var detailText: some View {
        if let detail {
            Text(detail)
                .font(.subheadline)
                .foregroundStyle(Color.togoMute)
        }
    }
}
