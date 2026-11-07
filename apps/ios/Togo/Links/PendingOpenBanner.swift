import SwiftUI
import TogoKit
import UIKit

/// A page the session's agent asked to open that the user has not used yet, kept in the chat
/// until it is used, dismissed or expires, so a sheet that was missed never strands it.
struct PendingOpenBanner: View {
    let open: PendingOpen
    @State private var copied = 0
    @State private var failed = false

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Image(systemName: "arrow.up.forward.app")
                    .foregroundStyle(Color.togoMute)
                    .accessibilityHidden(true)
                Text(LinkCopy.wantsToOpen(agent: open.agent, host: open.url.host(percentEncoded: false) ?? ""))
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(Color.togoSnow)
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
                Button(LinkCopy.dismiss, systemImage: "xmark") { LinkCenter.shared.requests.resolve(open.id) }
                    .labelStyle(.iconOnly)
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(Color.togoMute)
                    .frame(minWidth: 44, minHeight: 44)
                    .contentShape(.rect)
                    .buttonStyle(.plain)
            }
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 8) { buttons }
                VStack(alignment: .leading, spacing: 8) { buttons }
            }
        }
        .padding(.leading, 14)
        .padding([.trailing, .bottom], 14)
        .overlay {
            RoundedRectangle(cornerRadius: 16, style: .continuous)
                .strokeBorder(Color.togoLine)
        }
        .haptic(.done, trigger: copied)
        .haptic(.error, trigger: failed, when: { $0 })
        .accessibilityElement(children: .contain)
    }

    @ViewBuilder private var buttons: some View {
        Button(LinkCopy.open) { LinkCenter.shared.use(open) }
            .buttonStyle(.primary(.fit))
        Button(copied > 0 ? LinkCopy.copied : LinkCopy.copy) {
            UIPasteboard.general.url = open.url
            copied += 1
        }
        .buttonStyle(.secondary(.fit))
        .contentTransition(.interpolate)
        // motion: cause → effect — the tapped Copy says Copied in place
        .motion(.respond, value: copied)
        if LinkCenter.shared.openOnMac != nil {
            Button(LinkCopy.openOnMac) {
                Task {
                    do { try await LinkCenter.shared.sendToMac(open) } catch { failed = true }
                }
            }
            .buttonStyle(.secondary(.fit))
        }
    }
}
