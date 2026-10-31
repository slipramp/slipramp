import AuthenticationServices
import Combine
import Foundation
import UIKit

@MainActor
final class AppModel: ObservableObject {
    @Published private(set) var client: SliprampAPIClient?
    @Published private(set) var isRestoring = true
    @Published private(set) var isCheckingServer = false
    @Published private(set) var isConnecting = false
    @Published private(set) var serverCompatibility: ServerCompatibility?
    @Published private(set) var serverTransportSecurity: ServerTransportSecurity?
    @Published private(set) var pendingTrustedHTTPServer: URL?
    @Published var connectionError: String?

    private let credentialStore: CredentialStoring
    private let defaults: UserDefaults
    private let serverURLKey = "slipramp.server-url"
    private let trustedHTTPServerURLKey = "slipramp.trusted-http-server-url"
    private var didRestore = false
    private var authenticationSession: ASWebAuthenticationSession?
    private let authenticationPresentation = AuthenticationPresentationContextProvider()

    init(
        credentialStore: CredentialStoring = KeychainCredentialStore(),
        defaults: UserDefaults = .standard
    ) {
        self.credentialStore = credentialStore
        self.defaults = defaults
    }

    var suggestedServerURL: String {
        defaults.string(forKey: serverURLKey) ?? ""
    }

    func restore() async {
        guard !didRestore else {
            return
        }
        didRestore = true
        defer { isRestoring = false }

        guard let rawURL = defaults.string(forKey: serverURLKey) else {
            return
        }

        do {
            let (baseURL, transportSecurity) = try trustedServerURL(rawURL)
            _ = try await checkCompatibility(at: baseURL)
            guard let token = try credentialStore.loadToken() else {
                return
            }
            let restoredClient = SliprampAPIClient(baseURL: baseURL, token: token)
            _ = try await restoredClient.getInbox()
            serverTransportSecurity = transportSecurity
            client = restoredClient
        } catch {
            connectionError = "The saved connection could not be restored. \(error.localizedDescription)"
        }
    }

    func checkServer(serverURL: String) async {
        guard !isCheckingServer, !isConnecting else {
            return
        }

        isCheckingServer = true
        serverCompatibility = nil
        serverTransportSecurity = nil
        connectionError = nil
        defer { isCheckingServer = false }

        do {
            let baseURL = try SliprampAPIClient.normalizeServerURL(serverURL)
            let transportSecurity = try SliprampAPIClient.transportSecurity(for: baseURL)
            if transportSecurity == .trustedPrivateHTTP,
               !isTrustedHTTPServer(baseURL)
            {
                pendingTrustedHTTPServer = baseURL
                return
            }
            serverCompatibility = try await checkCompatibility(at: baseURL)
            serverTransportSecurity = transportSecurity
        } catch {
            connectionError = error.localizedDescription
        }
    }

    func confirmTrustedHTTPServer(_ baseURL: URL) async {
        defaults.set(baseURL.absoluteString, forKey: trustedHTTPServerURLKey)
        pendingTrustedHTTPServer = nil
        await checkServer(serverURL: baseURL.absoluteString)
    }

    func cancelTrustedHTTPServerConfirmation() {
        pendingTrustedHTTPServer = nil
    }

    func clearServerCompatibility() {
        serverCompatibility = nil
        serverTransportSecurity = nil
        pendingTrustedHTTPServer = nil
        connectionError = nil
    }

    func connect(serverURL: String, apiKey: String) async {
        guard !isConnecting else {
            return
        }

        isConnecting = true
        connectionError = nil
        defer { isConnecting = false }

        let token = apiKey.trimmingCharacters(in: .whitespacesAndNewlines)
        guard token.hasPrefix("slipramp_key_"), token.count > "slipramp_key_".count else {
            connectionError = "Enter a Slipramp API key beginning with slipramp_key_."
            return
        }

        do {
            let (baseURL, transportSecurity) = try trustedServerURL(serverURL)
            serverCompatibility = try await checkCompatibility(at: baseURL)
            serverTransportSecurity = transportSecurity
            let candidate = SliprampAPIClient(baseURL: baseURL, token: token)
            _ = try await candidate.getInbox()

            try credentialStore.saveToken(token)
            defaults.set(baseURL.absoluteString, forKey: serverURLKey)
            client = candidate
        } catch {
            connectionError = error.localizedDescription
        }
    }

    func signInWithOIDC(serverURL: String) async {
        guard !isConnecting else {
            return
        }

        isConnecting = true
        connectionError = nil
        defer { isConnecting = false }

        do {
            let (baseURL, transportSecurity) = try trustedServerURL(serverURL)
            let compatibility = try await checkCompatibility(at: baseURL)
            serverCompatibility = compatibility
            serverTransportSecurity = transportSecurity
            guard compatibility.authenticationMethods.contains("oidc-native") else {
                throw SliprampAPIError.nativeOIDCAuthenticationUnavailable
            }

            let apiClient = SliprampAPIClient(baseURL: baseURL)
            let pkce = NativeOIDCPKCE.make()
            let loginURL = try apiClient.nativeOIDCLoginURL(codeChallenge: pkce.challenge)
            let callbackURL = try await startAuthenticationSession(at: loginURL)
            let code = try SliprampAPIClient.nativeOIDCAuthorizationCode(from: callbackURL)
            let token = try await apiClient.exchangeNativeOIDCLogin(
                code: code,
                codeVerifier: pkce.verifier
            )
            let candidate = SliprampAPIClient(baseURL: baseURL, token: token)
            _ = try await candidate.getInbox()

            try credentialStore.saveToken(token)
            defaults.set(baseURL.absoluteString, forKey: serverURLKey)
            client = candidate
        } catch let error as ASWebAuthenticationSessionError
            where error.code == .canceledLogin {
            return
        } catch {
            connectionError = error.localizedDescription
        }
    }

    func disconnect() {
        connectionError = nil
        client = nil
        serverCompatibility = nil
        serverTransportSecurity = nil
        pendingTrustedHTTPServer = nil
        defaults.removeObject(forKey: serverURLKey)
        defaults.removeObject(forKey: trustedHTTPServerURLKey)

        do {
            try credentialStore.deleteToken()
        } catch {
            connectionError = error.localizedDescription
        }
    }

    private func checkCompatibility(at baseURL: URL) async throws -> ServerCompatibility {
        let compatibility = try await SliprampAPIClient(baseURL: baseURL).getClientCompatibility()
        try ClientCompatibilityPolicy.validate(compatibility)
        return compatibility
    }

    private func trustedServerURL(_ value: String) throws -> (URL, ServerTransportSecurity) {
        let baseURL = try SliprampAPIClient.normalizeServerURL(value)
        let transportSecurity = try SliprampAPIClient.transportSecurity(for: baseURL)
        if transportSecurity == .trustedPrivateHTTP, !isTrustedHTTPServer(baseURL) {
            throw SliprampAPIError.trustedPrivateHTTPConfirmationRequired
        }
        return (baseURL, transportSecurity)
    }

    private func isTrustedHTTPServer(_ baseURL: URL) -> Bool {
        defaults.string(forKey: trustedHTTPServerURLKey) == baseURL.absoluteString
    }

    private func startAuthenticationSession(at url: URL) async throws -> URL {
        try await withCheckedThrowingContinuation { continuation in
            let session = ASWebAuthenticationSession(
                url: url,
                callbackURLScheme: "slipramp"
            ) { [weak self] callbackURL, error in
                Task { @MainActor in
                    self?.authenticationSession = nil
                    if let callbackURL {
                        continuation.resume(returning: callbackURL)
                    } else if let error {
                        continuation.resume(throwing: error)
                    } else {
                        continuation.resume(throwing: SliprampAPIError.nativeOIDCSignInFailed)
                    }
                }
            }
            session.presentationContextProvider = authenticationPresentation
            authenticationSession = session
            if !session.start() {
                authenticationSession = nil
                continuation.resume(throwing: SliprampAPIError.nativeOIDCSignInFailed)
            }
        }
    }
}

@MainActor
private final class AuthenticationPresentationContextProvider: NSObject,
    ASWebAuthenticationPresentationContextProviding
{
    func presentationAnchor(for _: ASWebAuthenticationSession) -> ASPresentationAnchor {
        for case let scene as UIWindowScene in UIApplication.shared.connectedScenes {
            if let window = scene.windows.first(where: \.isKeyWindow) {
                return window
            }
        }
        return ASPresentationAnchor()
    }
}
