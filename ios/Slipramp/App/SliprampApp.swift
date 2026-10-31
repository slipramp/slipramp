import SwiftUI

@main
struct SliprampApp: App {
    @StateObject private var model = AppModel()

    var body: some Scene {
        WindowGroup {
            AppRootView()
                .environmentObject(model)
                .task {
                    await model.restore()
                }
        }
    }
}

private struct AppRootView: View {
    @EnvironmentObject private var model: AppModel
    @AppStorage(SliprampAppearance.storageKey) private var appearance = SliprampAppearance.system

    var body: some View {
        ZStack {
            SliprampScreenBackground()

            Group {
                if model.isRestoring {
                    SliprampLoadingState(message: "Connecting to Slipramp…")
                } else if let client = model.client {
                    MainTabView(client: client)
                } else {
                    ConnectionView()
                }
            }
        }
        .tint(SliprampTheme.accent)
        .preferredColorScheme(appearance.colorScheme)
        .animation(.easeInOut(duration: 0.25), value: model.client != nil)
    }
}
