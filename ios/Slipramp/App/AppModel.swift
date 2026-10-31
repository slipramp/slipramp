import Combine
import Foundation

@MainActor
final class AppModel: ObservableObject {
    @Published private(set) var client: SliprampAPIClient?
    @Published private(set) var isRestoring = true
    @Published private(set) var isConnecting = false
    @Published var connectionError: String?

    private let credentialStore: CredentialStoring
    private let defaults: UserDefaults
    private let serverURLKey = "slipramp.server-url"
    private var didRestore = false

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
            guard let token = try credentialStore.loadToken() else {
                return
            }
            let baseURL = try SliprampAPIClient.normalizeServerURL(rawURL)
            let restoredClient = SliprampAPIClient(baseURL: baseURL, token: token)
            _ = try await restoredClient.getInbox()
            client = restoredClient
        } catch {
            connectionError = "The saved connection could not be restored. \(error.localizedDescription)"
        }
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
            let baseURL = try SliprampAPIClient.normalizeServerURL(serverURL)
            let candidate = SliprampAPIClient(baseURL: baseURL, token: token)
            _ = try await candidate.getInbox()

            try credentialStore.saveToken(token)
            defaults.set(baseURL.absoluteString, forKey: serverURLKey)
            client = candidate
        } catch {
            connectionError = error.localizedDescription
        }
    }

    func disconnect() {
        connectionError = nil
        client = nil
        defaults.removeObject(forKey: serverURLKey)

        do {
            try credentialStore.deleteToken()
        } catch {
            connectionError = error.localizedDescription
        }
    }
}
