import SwiftUI

struct ConnectionView: View {
    @EnvironmentObject private var model: AppModel
    @State private var serverURL = ""
    @State private var apiKey = ""

    var body: some View {
        NavigationStack {
            ZStack {
                SliprampScreenBackground(decorative: true)

                ScrollView {
                    VStack(spacing: SliprampTheme.Space.xLarge) {
                        VStack(spacing: SliprampTheme.Space.medium) {
                            SliprampMascot()
                                .frame(width: 220, height: 145)

                            VStack(spacing: SliprampTheme.Space.small) {
                                Text("Slipramp")
                                    .font(.largeTitle.weight(.bold))
                                Text("Your control plane, in your pocket.")
                                    .font(.headline)
                                    .foregroundStyle(.secondary)
                            }
                        }

                        SliprampCard {
                            VStack(spacing: SliprampTheme.Space.large) {
                                SliprampField(label: "Server", symbol: "server.rack") {
                                    TextField("https://slipramp.example.com", text: $serverURL)
                                        .keyboardType(.URL)
                                        .textInputAutocapitalization(.never)
                                        .autocorrectionDisabled()
                                        .textContentType(.URL)
                                        .accessibilityLabel("Slipramp Server URL")
                                }

                                SliprampField(label: "API key", symbol: "key.fill") {
                                    SecureField("slipramp_key_…", text: $apiKey)
                                        .textInputAutocapitalization(.never)
                                        .autocorrectionDisabled()
                                        .textContentType(.password)
                                        .accessibilityLabel("Slipramp API key")
                                }

                                Text("Create a personal key in the web app with at least slipramp:read.")
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                                    .frame(maxWidth: .infinity, alignment: .leading)

                                if let error = model.connectionError {
                                    SliprampCallout(message: error, tone: .danger)
                                }

                                Button {
                                    Task {
                                        await model.connect(serverURL: serverURL, apiKey: apiKey)
                                    }
                                } label: {
                                    if model.isConnecting {
                                        ProgressView()
                                            .tint(.white)
                                            .accessibilityLabel("Connecting")
                                    } else {
                                        Label("Connect", systemImage: "arrow.right")
                                    }
                                }
                                .buttonStyle(SliprampPrimaryButtonStyle())
                                .disabled(model.isConnecting || serverURL.isEmpty || apiKey.isEmpty)
                            }
                        }

                        Label("Your API key stays in this device’s Keychain.", systemImage: "lock.fill")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .multilineTextAlignment(.center)
                    }
                    .frame(maxWidth: 540)
                    .padding(.horizontal, SliprampTheme.Space.large)
                    .padding(.top, SliprampTheme.Space.xLarge)
                    .padding(.bottom, 48)
                    .frame(maxWidth: .infinity)
                }
                .scrollDismissesKeyboard(.interactively)
            }
            .toolbar(.hidden, for: .navigationBar)
            .task {
                if serverURL.isEmpty {
                    serverURL = model.suggestedServerURL
                }
            }
        }
    }
}
