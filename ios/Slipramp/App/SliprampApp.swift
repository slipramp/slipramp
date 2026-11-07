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
                switch model.connectionState {
                case .restoring:
                    SliprampLoadingState(message: "Connecting to Slipramp…")
                case let .connected(client):
                    MainTabView(client: client)
                default:
                    ConnectionView()
                }
            }
        }
        .tint(SliprampTheme.accent)
        .preferredColorScheme(appearance.colorScheme)
        .animation(.easeInOut(duration: 0.25), value: model.connectionState.client != nil)
    }
}
