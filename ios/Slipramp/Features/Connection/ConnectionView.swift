import SwiftUI

struct ConnectionView: View {
    @EnvironmentObject private var model: AppModel
    @State private var serverURL = ""
    @State private var apiKey = ""
    @State private var showsAPIKey = false

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
                                        .disabled(connectionState.checkedServer != nil)
                                }

                                if let server = connectionState.checkedServer {
                                    SliprampCallout(
                                        message: "Slipramp \(server.compatibility.serverVersion) is compatible.",
                                        tone: .success,
                                        symbol: "checkmark.circle.fill"
                                    )

                                    if server.transportSecurity == .trustedPrivateHTTP {
                                        SliprampCallout(
                                            message: "Using HTTP on a trusted private network. "
                                                + "That network must provide transport encryption.",
                                            tone: .warning,
                                            symbol: "exclamationmark.shield.fill"
                                        )
                                    }

                                    Button("Use a different Server") {
                                        apiKey = ""
                                        showsAPIKey = false
                                        model.useDifferentServer()
                                    }
                                    .font(.subheadline.weight(.semibold))
                                    .foregroundStyle(SliprampTheme.accent)
                                    .frame(maxWidth: .infinity, alignment: .leading)

                                    if supportsNativeOIDC, !showsAPIKey {
                                        Button {
                                            Task {
                                                await model.signInWithOIDC()
                                            }
                                        } label: {
                                            if connectionState.isAuthenticating {
                                                ProgressView()
                                                    .tint(.white)
                                                    .accessibilityLabel("Signing in")
                                            } else {
                                                Label("Sign in with SSO", systemImage: "person.badge.key.fill")
                                                    .labelStyle(.titleAndIcon)
                                            }
                                        }
                                        .buttonStyle(SliprampPrimaryButtonStyle())
                                        .disabled(connectionState.isAuthenticating)

                                        Button("Use an API key instead") {
                                            showsAPIKey = true
                                        }
                                        .font(.subheadline.weight(.semibold))
                                        .foregroundStyle(SliprampTheme.accent)
                                    } else {
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

                                        Button {
                                            Task {
                                                await model.connect(apiKey: apiKey)
                                            }
                                        } label: {
                                            if connectionState.isAuthenticating {
                                                ProgressView()
                                                    .tint(.white)
                                                    .accessibilityLabel("Connecting")
                                            } else {
                                                Label("Connect", systemImage: "arrow.right")
                                                    .labelStyle(.titleAndIcon)
                                            }
                                        }
                                        .buttonStyle(SliprampPrimaryButtonStyle())
                                        .disabled(connectionState.isAuthenticating || apiKey.isEmpty)

                                        if supportsNativeOIDC {
                                            Button("Use SSO instead") {
                                                apiKey = ""
                                                showsAPIKey = false
                                            }
                                            .font(.subheadline.weight(.semibold))
                                            .foregroundStyle(SliprampTheme.accent)
                                        }
                                    }
                                }

                                if let error = connectionState.errorMessage {
                                    SliprampCallout(message: error, tone: .danger)
                                }

                                if connectionState.checkedServer == nil {
                                    Button {
                                        Task {
                                            await model.checkServer(serverURL: serverURL)
                                        }
                                    } label: {
                                        if connectionState.isCheckingServer {
                                            ProgressView()
                                                .tint(.white)
                                                .accessibilityLabel("Checking Server")
                                        } else {
                                            Label("Check Server", systemImage: "network")
                                                .labelStyle(.titleAndIcon)
                                        }
                                    }
                                    .buttonStyle(SliprampPrimaryButtonStyle())
                                    .disabled(connectionState.isCheckingServer || serverURL.isEmpty)
                                }
                            }
                        }

                        Label("Your sign-in credential stays in this device’s Keychain.", systemImage: "lock.fill")
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
            .alert(
                "Trust Private HTTP Server?",
                isPresented: trustedHTTPConfirmationPresented,
                presenting: connectionState.pendingTrustedHTTPServer
            ) { baseURL in
                Button("Cancel", role: .cancel) {
                    model.cancelTrustedHTTPServerConfirmation()
                }
                Button("Trust & Continue") {
                    Task {
                        await model.confirmTrustedHTTPServer(baseURL)
                    }
                }
            } message: { baseURL in
                Text(
                    "HTTP does not encrypt traffic itself. Continue only if "
                        + "\(baseURL.host ?? "this Server") is reached through a trusted "
                        + "encrypted network, such as a tailnet."
                )
            }
            .task {
                if serverURL.isEmpty {
                    serverURL = model.suggestedServerURL
                }
            }
        }
    }

    private var supportsNativeOIDC: Bool {
        connectionState.checkedServer?.compatibility.authenticationMethods.contains("oidc-native") == true
    }

    private var connectionState: ConnectionState {
        model.connectionState
    }

    private var trustedHTTPConfirmationPresented: Binding<Bool> {
        Binding(
            get: { connectionState.pendingTrustedHTTPServer != nil },
            set: { isPresented in
                if !isPresented {
                    model.cancelTrustedHTTPServerConfirmation()
                }
            }
        )
    }
}
