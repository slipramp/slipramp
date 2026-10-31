import SwiftUI

struct SettingsView: View {
    @EnvironmentObject private var model: AppModel
    @AppStorage(SliprampAppearance.storageKey) private var appearance = SliprampAppearance.system
    let client: SliprampAPIClient

    var body: some View {
        ZStack {
            SliprampScreenBackground()

            ScrollView {
                VStack(alignment: .leading, spacing: SliprampTheme.Space.xLarge) {
                    VStack(spacing: SliprampTheme.Space.small) {
                        SliprampMark()
                            .frame(width: 58, height: 58)
                        Text("Slipramp")
                            .font(.title2.weight(.bold))
                        Text("Native preview")
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: .infinity)

                    settingsSection(title: "Connection") {
                        SettingsRow(label: "Server", symbol: "server.rack") {
                            Text(client.baseURL.host() ?? client.baseURL.absoluteString)
                                .multilineTextAlignment(.trailing)
                        }
                        SettingsRow(label: "Credential", symbol: "key.fill") {
                            Text("Stored in Keychain")
                        }
                    }

                    settingsSection(title: "Appearance") {
                        VStack(alignment: .leading, spacing: SliprampTheme.Space.medium) {
                            Picker("Appearance", selection: $appearance) {
                                ForEach(SliprampAppearance.allCases) { option in
                                    Text(option.label)
                                        .tag(option)
                                }
                            }
                            .pickerStyle(.segmented)

                            Text(appearanceDescription)
                                .font(.caption)
                                .foregroundStyle(.secondary)
                        }
                    }

                    settingsSection(title: "About") {
                        Link(destination: URL(string: "https://github.com/slipramp/slipramp")!) {
                            HStack(spacing: SliprampTheme.Space.medium) {
                                Image(systemName: "book.closed.fill")
                                    .foregroundStyle(SliprampTheme.accent)
                                    .frame(width: 24)
                                Text("Slipramp documentation")
                                Spacer()
                                Image(systemName: "arrow.up.right")
                                    .font(.caption.weight(.semibold))
                                    .foregroundStyle(.secondary)
                            }
                        }
                    }

                    VStack(spacing: SliprampTheme.Space.medium) {
                        Button("Disconnect", role: .destructive) {
                            model.disconnect()
                        }
                        .buttonStyle(.bordered)
                        .controlSize(.large)

                        Text(
                            "Disconnecting removes the saved Server URL and API key from this device. "
                                + "It does not revoke the key on the Server."
                        )
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                    }
                    .frame(maxWidth: .infinity)
                }
                .padding(SliprampTheme.Space.large)
                .padding(.bottom, SliprampTheme.Space.xLarge)
            }
        }
        .navigationTitle("Settings")
    }

    private var appearanceDescription: String {
        switch appearance {
        case .system:
            "Slipramp follows this device’s appearance setting."
        case .light:
            "Slipramp always uses its light appearance."
        case .dark:
            "Slipramp always uses its dark appearance."
        }
    }

    private func settingsSection<Content: View>(
        title: String,
        @ViewBuilder content: () -> Content
    ) -> some View {
        VStack(alignment: .leading, spacing: SliprampTheme.Space.medium) {
            SliprampSectionHeader(title: title)
            SliprampCard {
                VStack(spacing: SliprampTheme.Space.large) {
                    content()
                }
            }
        }
    }
}

private struct SettingsRow<Content: View>: View {
    let label: String
    let symbol: String
    private let content: Content

    init(label: String, symbol: String, @ViewBuilder content: () -> Content) {
        self.label = label
        self.symbol = symbol
        self.content = content()
    }

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: SliprampTheme.Space.medium) {
            Image(systemName: symbol)
                .foregroundStyle(SliprampTheme.accent)
                .frame(width: 24)
            Text(label)
            Spacer(minLength: SliprampTheme.Space.medium)
            content
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
        .padding(.vertical, SliprampTheme.Space.xSmall)
        .accessibilityElement(children: .combine)
    }
}
