import SwiftUI
import WidgetKit

@main
struct TogoWidgets: WidgetBundle {
    var body: some Widget {
        TogoLiveActivity()
        WaitingWidget()
    }
}
