import SwiftUI
import TogoKit

/// What Togo does over plain SSH next to what the helper adds, then, under Learn more, what the
/// helper is and is not allowed to do. Side by side when there is room, one above the other at
/// accessibility sizes.
struct HelperComparison: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    /// Lines the status words up under the fact's text: the icon column plus its spacing.
    @ScaledMetric(relativeTo: .subheadline) private var statusInset: CGFloat = 26
    /// Opens Learn more, for snapshots.
    var expanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 28) {
            let layout = dynamicTypeSize.isAccessibilitySize
                ? AnyLayout(VStackLayout(alignment: .leading, spacing: 12))
                : AnyLayout(HStackLayout(alignment: .top, spacing: 12))
            layout {
                column(
                    String(localized: "tutorial.helper.without", defaultValue: "Without the helper"),
                    without
                )
                column(
                    String(localized: "tutorial.helper.with", defaultValue: "With the helper"),
                    with
                )
            }
            TutorialMore(expanded: expanded) { boundaries }
        }
    }

    private var without: some View {
        VStack(alignment: .leading, spacing: 12) {
            fact(String(localized: "tutorial.helper.without.ssh", defaultValue: "Plain SSH and tmux"))
            fact(String(localized: "tutorial.helper.without.attach", defaultValue: "Attach, or open a new shell"))
        }
    }

    private var with: some View {
        VStack(alignment: .leading, spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                fact(String(localized: "tutorial.helper.with.status", defaultValue: "Each agent's status"))
                ViewThatFits(in: .horizontal) {
                    HStack(spacing: 10) { statusWords }
                    VStack(alignment: .leading, spacing: 2) { statusWords }
                }
                .padding(.leading, statusInset)
            }
            fact(String(localized: "tutorial.helper.with.approve", defaultValue: "Approve from the lock screen"))
            fact(String(localized: "tutorial.helper.with.notifications", defaultValue: "Notifications"))
        }
    }

    @ViewBuilder private var statusWords: some View {
        StatusWord(status: .working)
        StatusWord(status: .needsYou)
        StatusWord(status: .done)
    }

    private var boundaries: some View {
        VStack(alignment: .leading, spacing: 16) {
            SectionHeading(String(localized: "tutorial.helper.limits", defaultValue: "What the helper does not do"))
            VStack(alignment: .leading, spacing: 16) {
                TutorialPoint(
                    String(localized: "tutorial.helper.port.title", defaultValue: "It never opens a network port"),
                    systemImage: "network.slash",
                    String(
                        localized: "tutorial.helper.port.detail",
                        defaultValue: "Togo reaches it only through your SSH login."
                    )
                )
                TutorialPoint(
                    String(localized: "tutorial.helper.user.title", defaultValue: "It runs as you"),
                    systemImage: "person",
                    String(
                        localized: "tutorial.helper.user.detail",
                        defaultValue: "No root, and nothing your own account could not do."
                    )
                )
                TutorialPoint(
                    title: String(localized: "tutorial.helper.source.title", defaultValue: "It is open source"),
                    systemImage: "chevron.left.forwardslash.chevron.right"
                ) {
                    SetupStepView(step: SetupStep(
                        text: String(
                            localized: "tutorial.helper.source.detail",
                            defaultValue: "It is MIT licensed, so you can read its source. To remove it, run:"
                        ),
                        command: "togo uninstall"
                    ))
                }
            }
        }
    }

    private func column(_ title: String, _ content: some View) -> some View {
        VStack(alignment: .leading, spacing: 14) {
            Text(title)
                .font(.headline)
                .foregroundStyle(Color.togoSnow)
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityAddTraits(.isHeader)
            content
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .background(Color.togoGrouped, in: .rect(cornerRadius: 16))
    }

    private func fact(_ text: String) -> some View {
        Label {
            Text(text)
                .font(.subheadline)
                .foregroundStyle(Color.togoSnow)
                .fixedSize(horizontal: false, vertical: true)
        } icon: {
            Image(systemName: "checkmark")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(Color.togoMute)
        }
        .labelStyle(FactLabelStyle())
    }
}

/// A fixed-width icon column, so wrapped lines line up under the first.
private struct FactLabelStyle: LabelStyle {
    @ScaledMetric(relativeTo: .subheadline) private var iconWidth: CGFloat = 18

    func makeBody(configuration: Configuration) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            configuration.icon.frame(width: iconWidth)
            configuration.title
        }
        .accessibilityElement(children: .combine)
    }
}

struct HelperPage: View {
    var expanded = false

    var body: some View {
        TutorialPageLayout(page: .helper) {
            HelperComparison(expanded: expanded)
        }
    }
}
