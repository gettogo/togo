import SwiftUI

struct AppUpdateNotice: View {
    @Environment(\.dynamicTypeSize) private var typeSize
    let version: String
    var dismiss: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Label(settingsText("appUpdate.available", "Togo \(version) is available"), systemImage: "arrow.down.circle")
                .font(.subheadline)
                .fixedSize(horizontal: false, vertical: true)
            actions {
                Link(destination: AppStoreLookup.storeURL) {
                    Text(settingsText("appUpdate.store", "App Store"))
                        .frame(minWidth: 44, minHeight: 44)
                        .contentShape(Rectangle())
                }
                .accessibilityLabel(settingsText("appUpdate.open", "Open Togo in the App Store"))
                Button(action: dismiss) {
                    Text(settingsText("appUpdate.dismiss", "Dismiss"))
                        .frame(minWidth: 44, minHeight: 44)
                        .contentShape(Rectangle())
                }
                .accessibilityLabel(settingsText("appUpdate.dismissLabel", "Dismiss this app update"))
            }
            .font(.subheadline)
            .buttonStyle(.borderless)
        }
        .foregroundStyle(Color.togoSnow)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("roster.appUpdate")
    }

    private var actions: AnyLayout {
        typeSize.isAccessibilitySize ? AnyLayout(VStackLayout(alignment: .leading, spacing: 0))
            : AnyLayout(HStackLayout(spacing: 16))
    }
}
