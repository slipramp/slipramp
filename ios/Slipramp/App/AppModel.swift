import AuthenticationServices
import Combine
import Foundation
#if canImport(UIKit)
import UIKit
#endif

struct CheckedServer {
    let baseURL: URL
    let compatibility: ServerCompatibility
    let transportSecurity: ServerTransportSecurity
}

enum ConnectionAuthenticationMethod: Equatable {
    case apiKey
    case nativeOIDC
}

enum ConnectionState {
    case restoring(operationID: UUID)
    case disconnected(error: String?)
    case checkingServer(
        baseURL: URL,
        transportSecurity: ServerTransportSecurity,
        operationID: UUID
    )
    case awaitingTrustedHTTPConfirmation(baseURL: URL)
    case ready(server: CheckedServer, error: String?)
    case authenticating(
        server: CheckedServer,
        method: ConnectionAuthenticationMethod,
        operationID: UUID
    )
    case connected(client: SliprampAPIClient)

    var checkedServer: CheckedServer? {
        switch self {
        case let .ready(server, _), let .authenticating(server, _, _):
            return server
        default:
            return nil
        }
    }

    var errorMessage: String? {
        switch self {
        case let .disconnected(error), let .ready(_, error):
            return error
        default:
            return nil
        }
    }

    var isCheckingServer: Bool {
        if case .checkingServer = self {
            return true
        }
        return false
    }

    var isAuthenticating: Bool {
        if case .authenticating = self {
            return true
        }
        return false
    }

    var pendingTrustedHTTPServer: URL? {
        if case let .awaitingTrustedHTTPConfirmation(baseURL) = self {
            return baseURL
        }
        return nil
    }

    var client: SliprampAPIClient? {
        if case let .connected(client) = self {
            return client
        }
        return nil
    }

    func isCurrent(operationID: UUID) -> Bool {
        switch self {
        case let .restoring(currentID),
             let .checkingServer(_, _, currentID),
             let .authenticating(_, _, currentID):
            return currentID == operationID
        default:
            return false
        }
    }
}

@MainActor
final class AppModel: ObservableObject {
    typealias AuthenticationSessionRunner = @MainActor (URL) async throws -> URL

    @Published private(set) var connectionState: ConnectionState

    private let credentialStore: CredentialStoring
    private let defaults: UserDefaults
    private let clientSession: URLSession?
    private let authenticationSessionRunner: AuthenticationSessionRunner?
    private let serverURLKey = "slipramp.server-url"
    private let trustedHTTPServerURLKey = "slipramp.trusted-http-server-url"

#if canImport(UIKit)
    private var authenticationSession: ASWebAuthenticationSession?
    private var authenticationSessionID: UUID?
    private let authenticationPresentation = AuthenticationPresentationContextProvider()
#endif

    init(
        credentialStore: CredentialStoring = KeychainCredentialStore(),
        defaults: UserDefaults = .standard,
        clientSession: URLSession? = nil,
        authenticationSessionRunner: AuthenticationSessionRunner? = nil
    ) {
        self.credentialStore = credentialStore
        self.defaults = defaults
        self.clientSession = clientSession
        self.authenticationSessionRunner = authenticationSessionRunner
        connectionState = .restoring(operationID: UUID())
    }

    var suggestedServerURL: String {
        defaults.string(forKey: serverURLKey) ?? ""
    }

    func restore() async {
        guard case let .restoring(operationID) = connectionState else {
            return
        }

        guard let rawURL = defaults.string(forKey: serverURLKey) else {
            finish(operationID, with: .disconnected(error: nil))
            return
        }

        do {
            let (baseURL, _) = try trustedServerURL(rawURL)
            _ = try await checkCompatibility(at: baseURL)
            guard connectionState.isCurrent(operationID: operationID) else {
                return
            }
            guard let token = try credentialStore.loadToken() else {
                connectionState = .disconnected(error: nil)
                return
            }

            let restoredClient = makeClient(baseURL: baseURL, token: token)
            _ = try await restoredClient.getInbox()
            finish(operationID, with: .connected(client: restoredClient))
        } catch {
            finish(
                operationID,
                with: .disconnected(
                    error: "The saved connection could not be restored. \(error.localizedDescription)"
                )
            )
        }
    }

    func checkServer(serverURL: String) async {
        cancelAuthenticationSession()

        let baseURL: URL
        let transportSecurity: ServerTransportSecurity
        do {
            baseURL = try SliprampAPIClient.normalizeServerURL(serverURL)
            transportSecurity = try SliprampAPIClient.transportSecurity(for: baseURL)
        } catch {
            connectionState = .disconnected(error: error.localizedDescription)
            return
        }

        if transportSecurity == .trustedPrivateHTTP, !isTrustedHTTPServer(baseURL) {
            connectionState = .awaitingTrustedHTTPConfirmation(baseURL: baseURL)
            return
        }

        let operationID = UUID()
        connectionState = .checkingServer(
            baseURL: baseURL,
            transportSecurity: transportSecurity,
            operationID: operationID
        )

        do {
            let compatibility = try await checkCompatibility(at: baseURL)
            let server = CheckedServer(
                baseURL: baseURL,
                compatibility: compatibility,
                transportSecurity: transportSecurity
            )
            finish(operationID, with: .ready(server: server, error: nil))
        } catch {
            finish(operationID, with: .disconnected(error: error.localizedDescription))
        }
    }

    func confirmTrustedHTTPServer(_ baseURL: URL) async {
        guard case let .awaitingTrustedHTTPConfirmation(pendingURL) = connectionState,
              pendingURL == baseURL
        else {
            return
        }

        defaults.set(baseURL.absoluteString, forKey: trustedHTTPServerURLKey)
        await checkServer(serverURL: baseURL.absoluteString)
    }

    func cancelTrustedHTTPServerConfirmation() {
        guard case .awaitingTrustedHTTPConfirmation = connectionState else {
            return
        }
        connectionState = .disconnected(error: nil)
    }

    func useDifferentServer() {
        cancelAuthenticationSession()
        connectionState = .disconnected(error: nil)
    }

    func connect(apiKey: String) async {
        guard let server = connectionState.checkedServer else {
            return
        }

        cancelAuthenticationSession()
        let token = apiKey.trimmingCharacters(in: .whitespacesAndNewlines)
        guard token.hasPrefix("slipramp_key_"), token.count > "slipramp_key_".count else {
            connectionState = .ready(
                server: server,
                error: "Enter a Slipramp API key beginning with slipramp_key_."
            )
            return
        }

        let operationID = UUID()
        connectionState = .authenticating(
            server: server,
            method: .apiKey,
            operationID: operationID
        )

        do {
            let candidate = makeClient(baseURL: server.baseURL, token: token)
            _ = try await candidate.getInbox()
            guard connectionState.isCurrent(operationID: operationID) else {
                return
            }

            try credentialStore.saveToken(token)
            defaults.set(server.baseURL.absoluteString, forKey: serverURLKey)
            connectionState = .connected(client: candidate)
        } catch {
            finish(
                operationID,
                with: .ready(server: server, error: error.localizedDescription)
            )
        }
    }

    func signInWithOIDC() async {
        guard let server = connectionState.checkedServer else {
            return
        }

        guard server.compatibility.authenticationMethods.contains("oidc-native") else {
            connectionState = .ready(
                server: server,
                error: SliprampAPIError.nativeOIDCAuthenticationUnavailable.localizedDescription
            )
            return
        }

        cancelAuthenticationSession()
        let operationID = UUID()
        connectionState = .authenticating(
            server: server,
            method: .nativeOIDC,
            operationID: operationID
        )

        do {
            let apiClient = makeClient(baseURL: server.baseURL)
            let pkce = NativeOIDCPKCE.make()
            let loginURL = try apiClient.nativeOIDCLoginURL(codeChallenge: pkce.challenge)
            let callbackURL = try await runAuthenticationSession(at: loginURL)
            guard connectionState.isCurrent(operationID: operationID) else {
                return
            }

            let code = try SliprampAPIClient.nativeOIDCAuthorizationCode(from: callbackURL)
            let token = try await apiClient.exchangeNativeOIDCLogin(
                code: code,
                codeVerifier: pkce.verifier
            )
            let candidate = makeClient(baseURL: server.baseURL, token: token)
            _ = try await candidate.getInbox()
            guard connectionState.isCurrent(operationID: operationID) else {
                return
            }

            try credentialStore.saveToken(token)
            defaults.set(server.baseURL.absoluteString, forKey: serverURLKey)
            connectionState = .connected(client: candidate)
        } catch {
            guard connectionState.isCurrent(operationID: operationID) else {
                return
            }
            if isAuthenticationCancellation(error) {
                connectionState = .ready(server: server, error: nil)
            } else {
                connectionState = .ready(server: server, error: error.localizedDescription)
            }
        }
    }

    func disconnect() {
        cancelAuthenticationSession()
        connectionState = .disconnected(error: nil)
        defaults.removeObject(forKey: serverURLKey)
        defaults.removeObject(forKey: trustedHTTPServerURLKey)

        do {
            try credentialStore.deleteToken()
        } catch {
            connectionState = .disconnected(error: error.localizedDescription)
        }
    }

    private func finish(_ operationID: UUID, with newState: ConnectionState) {
        guard connectionState.isCurrent(operationID: operationID) else {
            return
        }
        connectionState = newState
    }

    private func checkCompatibility(at baseURL: URL) async throws -> ServerCompatibility {
        let compatibility = try await makeClient(baseURL: baseURL).getClientCompatibility()
        try ClientCompatibilityPolicy.validate(compatibility)
        return compatibility
    }

    private func makeClient(baseURL: URL, token: String = "") -> SliprampAPIClient {
        SliprampAPIClient(baseURL: baseURL, token: token, session: clientSession)
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

    private func runAuthenticationSession(at url: URL) async throws -> URL {
        if let authenticationSessionRunner {
            return try await authenticationSessionRunner(url)
        }
#if canImport(UIKit)
        return try await startAuthenticationSession(at: url)
#else
        throw SliprampAPIError.nativeOIDCSignInFailed
#endif
    }

    private func isAuthenticationCancellation(_ error: Error) -> Bool {
        let authenticationError = error as NSError
        return authenticationError.domain == ASWebAuthenticationSessionError.errorDomain &&
            authenticationError.code == ASWebAuthenticationSessionError.Code.canceledLogin.rawValue
    }

    private func cancelAuthenticationSession() {
#if canImport(UIKit)
        authenticationSession?.cancel()
        authenticationSession = nil
        authenticationSessionID = nil
#endif
    }

#if canImport(UIKit)
    private func startAuthenticationSession(at url: URL) async throws -> URL {
        try await withCheckedThrowingContinuation { continuation in
            let sessionID = UUID()
            let session = ASWebAuthenticationSession(
                url: url,
                callbackURLScheme: "slipramp"
            ) { [weak self] callbackURL, error in
                Task { @MainActor in
                    if self?.authenticationSessionID == sessionID {
                        self?.authenticationSession = nil
                        self?.authenticationSessionID = nil
                    }
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
            authenticationSessionID = sessionID
            if !session.start() {
                authenticationSession = nil
                authenticationSessionID = nil
                continuation.resume(throwing: SliprampAPIError.nativeOIDCSignInFailed)
            }
        }
    }
#endif
}

#if canImport(UIKit)
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
#endif
