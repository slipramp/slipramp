import SwiftUI

struct MainTabView: View {
    let client: SliprampAPIClient
    @State private var productDataRevision = 0

    var body: some View {
        TabView {
            NavigationStack {
                InboxView(
                    client: client,
                    refreshRevision: productDataRevision,
                    onRunChanged: invalidateProductData
                )
                .navigationBarTitleDisplayMode(.inline)
            }
            .tabItem {
                Label("Inbox", systemImage: "tray.full")
            }

            NavigationStack {
                TargetListView(
                    client: client,
                    onRunChanged: invalidateProductData
                )
                .navigationBarTitleDisplayMode(.inline)
            }
            .tabItem {
                Label("Targets", systemImage: "scope")
            }

            NavigationStack {
                RunListView(
                    client: client,
                    refreshRevision: productDataRevision,
                    onRunChanged: invalidateProductData
                )
                .navigationBarTitleDisplayMode(.inline)
            }
            .tabItem {
                Label("Runs", systemImage: "clock.arrow.circlepath")
            }

            NavigationStack {
                PromotionListView(
                    client: client,
                    refreshRevision: productDataRevision,
                    onPromotionChanged: invalidateProductData
                )
                .navigationBarTitleDisplayMode(.inline)
            }
            .tabItem {
                Label("Promotions", systemImage: "arrow.up.right.circle")
            }

            NavigationStack {
                SettingsView(client: client)
                    .navigationBarTitleDisplayMode(.inline)
            }
            .tabItem {
                Label("Settings", systemImage: "gearshape")
            }
        }
        .tint(SliprampTheme.accent)
        .toolbarBackground(.ultraThinMaterial, for: .tabBar)
        .toolbarBackground(.visible, for: .tabBar)
    }

    private func invalidateProductData() {
        productDataRevision += 1
    }
}
