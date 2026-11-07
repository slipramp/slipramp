import AuthenticationServices
import Foundation
import XCTest
@testable import SliprampCore

@MainActor
final class AppModelTests: XCTestCase {
    override func tearDown() {
        AppModelURLProtocol.requestHandler = nil
        AppModelURLProtocol.responseDelay = nil
        super.tearDown()
    }

    func testRestoreWithoutSavedURLBecomesDisconnected() async {
        let (model, _, _) = makeModel()

        await model.restore()

        assertDisconnected(model.connectionState, error: nil)
    }

    func testRestoreValidSessionConnects() async throws {
        let (model, defaults, credentials) = makeModel(token: "slipramp_key_saved")
        defaults.set("https://saved.example", forKey: serverURLKey)
        AppModelURLProtocol.requestHandler = { request in
            switch request.url?.path {
            case "/api/v1/client-compatibility":
                return self.compatibilityResponse(for: request)
            case "/api/v1/inbox":
                XCTAssertEqual(
                    request.value(forHTTPHeaderField: "Authorization"),
                    "Bearer slipramp_key_saved"
                )
                return self.inboxResponse(for: request)
            default:
                throw TestError.unexpectedRequest
            }
        }

        await model.restore()

        let client = try connectedClient(model.connectionState)
        XCTAssertEqual(client.baseURL.absoluteString, "https://saved.example")
        XCTAssertEqual(credentials.token, "slipramp_key_saved")
    }

    func testRestoreReportsCredentialAndCompatibilityErrors() async {
        let (expiredModel, expiredDefaults, expiredCredentials) = makeModel(
            token: "slipramp_key_expired"
        )
        expiredDefaults.set("https://saved.example", forKey: serverURLKey)
        AppModelURLProtocol.requestHandler = { request in
            if request.url?.path == "/api/v1/client-compatibility" {
                return self.compatibilityResponse(for: request)
            }
            return self.problemResponse(
                for: request,
                status: 401,
                detail: "The credential is expired."
            )
        }

        await expiredModel.restore()

        let credentialError = try? disconnectedError(expiredModel.connectionState)
        XCTAssertTrue(credentialError?.contains("saved connection could not be restored") == true)
        XCTAssertTrue(credentialError?.contains("credential is expired") == true)
        XCTAssertEqual(
            expiredDefaults.string(forKey: serverURLKey),
            "https://saved.example"
        )
        XCTAssertEqual(expiredCredentials.token, "slipramp_key_expired")

        let (incompatibleModel, incompatibleDefaults, _) = makeModel(
            token: "slipramp_key_saved"
        )
        incompatibleDefaults.set("https://saved.example", forKey: serverURLKey)
        AppModelURLProtocol.requestHandler = { request in
            self.compatibilityResponse(for: request, contract: 2)
        }

        await incompatibleModel.restore()

        let compatibilityError = try? disconnectedError(incompatibleModel.connectionState)
        XCTAssertTrue(compatibilityError?.contains("saved connection could not be restored") == true)
        XCTAssertTrue(compatibilityError?.contains("newer version of Slipramp for iOS") == true)
    }

    func testServerChecksHandleHTTPSAndPrivateHTTPTrust() async throws {
        let (httpsModel, _, _) = makeModel()
        AppModelURLProtocol.requestHandler = { request in
            self.compatibilityResponse(for: request)
        }

        await httpsModel.checkServer(serverURL: "https://slipramp.example")

        let httpsServer = try readyServer(httpsModel.connectionState)
        XCTAssertEqual(httpsServer.baseURL.absoluteString, "https://slipramp.example")
        XCTAssertEqual(httpsServer.transportSecurity, .https)
        XCTAssertEqual(httpsServer.compatibility.serverVersion, "test-version")

        let (trustedHTTPModel, trustedHTTPDefaults, _) = makeModel()
        let baseURL = URL(string: "http://slipramp.local:8080")!

        await trustedHTTPModel.checkServer(serverURL: baseURL.absoluteString)
        guard case let .awaitingTrustedHTTPConfirmation(pendingURL) =
            trustedHTTPModel.connectionState
        else {
            return XCTFail("Expected trusted HTTP confirmation")
        }
        XCTAssertEqual(pendingURL, baseURL)

        await trustedHTTPModel.confirmTrustedHTTPServer(baseURL)

        let trustedHTTPServer = try readyServer(trustedHTTPModel.connectionState)
        XCTAssertEqual(trustedHTTPServer.transportSecurity, .trustedPrivateHTTP)
        XCTAssertEqual(
            trustedHTTPDefaults.string(forKey: trustedHTTPServerURLKey),
            baseURL.absoluteString
        )

        let (canceledHTTPModel, canceledHTTPDefaults, _) = makeModel()

        await canceledHTTPModel.checkServer(serverURL: baseURL.absoluteString)
        canceledHTTPModel.cancelTrustedHTTPServerConfirmation()

        assertDisconnected(canceledHTTPModel.connectionState, error: nil)
        XCTAssertNil(canceledHTTPDefaults.string(forKey: trustedHTTPServerURLKey))
    }

    func testSuccessfulAPIKeyConnectionSavesAndDisconnectClearsConnection() async throws {
        let (model, defaults, credentials) = makeModel()
        AppModelURLProtocol.requestHandler = { request in
            if request.url?.path == "/api/v1/client-compatibility" {
                return self.compatibilityResponse(for: request)
            }
            XCTAssertEqual(
                request.value(forHTTPHeaderField: "Authorization"),
                "Bearer slipramp_key_valid"
            )
            return self.inboxResponse(for: request)
        }
        await model.checkServer(serverURL: "https://slipramp.example")

        await model.connect(apiKey: " slipramp_key_valid ")

        _ = try connectedClient(model.connectionState)
        XCTAssertEqual(credentials.token, "slipramp_key_valid")
        XCTAssertEqual(defaults.string(forKey: serverURLKey), "https://slipramp.example")

        model.disconnect()

        assertDisconnected(model.connectionState, error: nil)
        XCTAssertTrue(credentials.didDelete)
        XCTAssertNil(credentials.token)
        XCTAssertNil(defaults.string(forKey: serverURLKey))
    }

    func testFailedAPIKeyAttemptsReturnToReadyForRetry() async throws {
        let requests = LockedCounter()
        let (model, defaults, credentials) = makeModel()
        AppModelURLProtocol.requestHandler = { request in
            _ = requests.increment()
            if request.url?.path == "/api/v1/client-compatibility" {
                return self.compatibilityResponse(for: request)
            }
            return self.problemResponse(
                for: request,
                status: 401,
                detail: "The API key is invalid."
            )
        }
        await model.checkServer(serverURL: "https://slipramp.example")

        await model.connect(apiKey: "not-a-slipramp-key")

        _ = try readyServer(model.connectionState)
        XCTAssertEqual(
            model.connectionState.errorMessage,
            "Enter a Slipramp API key beginning with slipramp_key_."
        )
        XCTAssertEqual(requests.currentValue, 1)

        await model.connect(apiKey: "slipramp_key_invalid")

        _ = try readyServer(model.connectionState)
        XCTAssertEqual(model.connectionState.errorMessage, "The API key is invalid.")
        XCTAssertEqual(requests.currentValue, 2)
        XCTAssertNil(credentials.token)
        XCTAssertNil(defaults.string(forKey: serverURLKey))
    }

    func testSuccessfulOIDCUsesAuthenticationCallbackAndSavesSession() async throws {
        var presentedLoginURL: URL?
        let authenticationRunner: AppModel.AuthenticationSessionRunner = { loginURL in
            presentedLoginURL = loginURL
            return URL(string: "slipramp://auth/callback?code=oidc-grant")!
        }
        let (model, defaults, credentials) = makeModel(
            authenticationSessionRunner: authenticationRunner
        )
        AppModelURLProtocol.requestHandler = { request in
            switch request.url?.path {
            case "/api/v1/client-compatibility":
                return self.compatibilityResponse(for: request, supportsOIDC: true)
            case "/api/v1/auth/native/exchange":
                return self.oidcExchangeResponse(for: request, token: "native-session-token")
            case "/api/v1/inbox":
                XCTAssertEqual(
                    request.value(forHTTPHeaderField: "Authorization"),
                    "Bearer native-session-token"
                )
                return self.inboxResponse(for: request)
            default:
                throw TestError.unexpectedRequest
            }
        }
        await model.checkServer(serverURL: "https://slipramp.example")

        await model.signInWithOIDC()

        if let error = model.connectionState.errorMessage {
            XCTFail("OIDC sign-in failed: \(error)")
        }
        _ = try connectedClient(model.connectionState)
        let loginComponents = try XCTUnwrap(
            URLComponents(url: XCTUnwrap(presentedLoginURL), resolvingAgainstBaseURL: false)
        )
        XCTAssertEqual(loginComponents.path, "/api/v1/auth/native/login")
        XCTAssertEqual(loginComponents.queryItems?.first?.name, "codeChallenge")
        XCTAssertEqual(loginComponents.queryItems?.first?.value?.count, 43)
        XCTAssertEqual(credentials.token, "native-session-token")
        XCTAssertEqual(defaults.string(forKey: serverURLKey), "https://slipramp.example")
    }

    func testFailedAndCanceledOIDCReturnToReady() async throws {
        let failedAuthentication: AppModel.AuthenticationSessionRunner = { _ in
            throw TestError.authenticationFailed
        }
        let (failedModel, _, failedCredentials) = makeModel(
            authenticationSessionRunner: failedAuthentication
        )
        AppModelURLProtocol.requestHandler = { request in
            self.compatibilityResponse(for: request, supportsOIDC: true)
        }
        await failedModel.checkServer(serverURL: "https://slipramp.example")

        await failedModel.signInWithOIDC()

        _ = try readyServer(failedModel.connectionState)
        XCTAssertEqual(
            failedModel.connectionState.errorMessage,
            TestError.authenticationFailed.localizedDescription
        )
        XCTAssertNil(failedCredentials.token)

        let canceledAuthentication: AppModel.AuthenticationSessionRunner = { _ in
            throw NSError(
                domain: ASWebAuthenticationSessionError.errorDomain,
                code: ASWebAuthenticationSessionError.Code.canceledLogin.rawValue
            )
        }
        let (canceledModel, _, canceledCredentials) = makeModel(
            authenticationSessionRunner: canceledAuthentication
        )
        await canceledModel.checkServer(serverURL: "https://slipramp.example")

        await canceledModel.signInWithOIDC()

        _ = try readyServer(canceledModel.connectionState)
        XCTAssertNil(canceledModel.connectionState.errorMessage)
        XCTAssertNil(canceledCredentials.token)
    }

    func testDisconnectDuringOIDCPreventsCallbackFromResumingConnection() async {
        let callback = SuspendedAuthenticationSession()
        let authenticationRunner: AppModel.AuthenticationSessionRunner = { _ in
            try await callback.run()
        }
        let (model, defaults, credentials) = makeModel(
            authenticationSessionRunner: authenticationRunner
        )
        AppModelURLProtocol.requestHandler = { request in
            self.compatibilityResponse(for: request, supportsOIDC: true)
        }
        await model.checkServer(serverURL: "https://slipramp.example")

        let signIn = Task {
            await model.signInWithOIDC()
        }
        await Task.yield()
        guard case .authenticating(_, .nativeOIDC, _) = model.connectionState else {
            return XCTFail("Expected native OIDC authentication")
        }

        model.disconnect()
        callback.succeed()
        await signIn.value

        assertDisconnected(model.connectionState, error: nil)
        XCTAssertTrue(credentials.didDelete)
        XCTAssertNil(credentials.token)
        XCTAssertNil(defaults.string(forKey: serverURLKey))
    }

    func testDisconnectDuringCheckPreventsStaleCompletion() async {
        let requestStarted = DispatchSemaphore(value: 0)
        let (model, defaults, credentials) = makeModel(token: "slipramp_key_saved")
        defaults.set("https://saved.example", forKey: serverURLKey)
        AppModelURLProtocol.requestHandler = { request in
            requestStarted.signal()
            return self.compatibilityResponse(for: request)
        }
        AppModelURLProtocol.responseDelay = { _ in 0.1 }

        let check = Task {
            await model.checkServer(serverURL: "https://slow.example")
        }
        await Task.yield()
        XCTAssertEqual(requestStarted.wait(timeout: .now() + 1), .success)

        model.disconnect()
        await check.value

        assertDisconnected(model.connectionState, error: nil)
        XCTAssertTrue(credentials.didDelete)
        XCTAssertNil(defaults.string(forKey: serverURLKey))
    }

    func testNewerServerCheckWinsOverOlderCompletion() async throws {
        let firstRequestStarted = DispatchSemaphore(value: 0)
        let (model, _, _) = makeModel()
        AppModelURLProtocol.requestHandler = { request in
            if request.url?.host == "first.example" {
                firstRequestStarted.signal()
                return self.compatibilityResponse(for: request, version: "first")
            }
            return self.compatibilityResponse(for: request, version: "second")
        }
        AppModelURLProtocol.responseDelay = { request in
            request.url?.host == "first.example" ? 0.1 : 0
        }

        let firstCheck = Task {
            await model.checkServer(serverURL: "https://first.example")
        }
        await Task.yield()
        XCTAssertEqual(firstRequestStarted.wait(timeout: .now() + 1), .success)

        await model.checkServer(serverURL: "https://second.example")
        await firstCheck.value

        let server = try readyServer(model.connectionState)
        XCTAssertEqual(server.baseURL.host, "second.example")
        XCTAssertEqual(server.compatibility.serverVersion, "second")
    }

    func testCompatibilityErrorCanBeRetried() async throws {
        let attempts = LockedCounter()
        let (model, _, _) = makeModel()
        AppModelURLProtocol.requestHandler = { request in
            if attempts.increment() == 1 {
                throw URLError(.cannotConnectToHost)
            }
            return self.compatibilityResponse(for: request)
        }

        await model.checkServer(serverURL: "https://retry.example")
        XCTAssertNotNil(try? disconnectedError(model.connectionState))

        await model.checkServer(serverURL: "https://retry.example")

        let server = try readyServer(model.connectionState)
        XCTAssertEqual(server.baseURL.host, "retry.example")
        XCTAssertNil(model.connectionState.errorMessage)
    }

    private var serverURLKey: String { "slipramp.server-url" }
    private var trustedHTTPServerURLKey: String { "slipramp.trusted-http-server-url" }

    private func makeModel(
        token: String? = nil,
        authenticationSessionRunner: AppModel.AuthenticationSessionRunner? = nil
    ) -> (AppModel, UserDefaults, TestCredentialStore) {
        let suiteName = "AppModelTests.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suiteName)!
        defaults.removePersistentDomain(forName: suiteName)
        let credentials = TestCredentialStore(token: token)
        let model = AppModel(
            credentialStore: credentials,
            defaults: defaults,
            clientSession: makeSession(),
            authenticationSessionRunner: authenticationSessionRunner
        )
        return (model, defaults, credentials)
    }

    private func makeSession() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [AppModelURLProtocol.self]
        return URLSession(configuration: configuration)
    }

    private func compatibilityResponse(
        for request: URLRequest,
        contract: Int = 1,
        supportsOIDC: Bool = false,
        version: String = "test-version"
    ) -> (HTTPURLResponse, Data) {
        let methods = supportsOIDC ? #"["api-key","oidc-native"]"# : #"["api-key"]"#
        let body = Data(
            """
            {
              "serverVersion": "\(version)",
              "productApiContract": \(contract),
              "authenticationMethods": \(methods)
            }
            """.utf8
        )
        return (response(for: request, status: 200), body)
    }

    private func inboxResponse(for request: URLRequest) -> (HTTPURLResponse, Data) {
        let body = Data(
            """
            {
              "generatedAt": "2026-11-07T12:00:00Z",
              "capabilities": { "acknowledgeProblems": true },
              "decisions": [],
              "problems": [],
              "activity": { "queued": 0, "starting": 0, "running": 0 }
            }
            """.utf8
        )
        return (response(for: request, status: 200), body)
    }

    private func oidcExchangeResponse(
        for request: URLRequest,
        token: String
    ) -> (HTTPURLResponse, Data) {
        let body = Data(
            """
            {
              "token": "\(token)",
              "expiresAt": "2026-11-08T12:00:00Z"
            }
            """.utf8
        )
        return (response(for: request, status: 200), body)
    }

    private func problemResponse(
        for request: URLRequest,
        status: Int,
        detail: String
    ) -> (HTTPURLResponse, Data) {
        let body = Data(
            """
            {
              "type": "about:blank",
              "title": "Unauthorized",
              "status": \(status),
              "detail": "\(detail)"
            }
            """.utf8
        )
        return (response(for: request, status: status), body)
    }

    private func response(for request: URLRequest, status: Int) -> HTTPURLResponse {
        HTTPURLResponse(
            url: request.url!,
            statusCode: status,
            httpVersion: "HTTP/1.1",
            headerFields: [
                "Content-Type": status < 400
                    ? "application/json"
                    : "application/problem+json",
                "Cache-Control": "private, no-store",
            ]
        )!
    }

    private func readyServer(_ state: ConnectionState) throws -> CheckedServer {
        guard case let .ready(server, _) = state else {
            throw TestError.unexpectedState
        }
        return server
    }

    private func connectedClient(_ state: ConnectionState) throws -> SliprampAPIClient {
        guard case let .connected(client) = state else {
            throw TestError.unexpectedState
        }
        return client
    }

    private func disconnectedError(_ state: ConnectionState) throws -> String {
        guard case let .disconnected(error?) = state else {
            throw TestError.unexpectedState
        }
        return error
    }

    private func assertDisconnected(
        _ state: ConnectionState,
        error expectedError: String?,
        file: StaticString = #filePath,
        line: UInt = #line
    ) {
        guard case let .disconnected(error) = state else {
            return XCTFail("Expected disconnected state", file: file, line: line)
        }
        XCTAssertEqual(error, expectedError, file: file, line: line)
    }
}

private final class TestCredentialStore: CredentialStoring {
    var token: String?
    private(set) var didDelete = false

    init(token: String?) {
        self.token = token
    }

    func loadToken() throws -> String? {
        token
    }

    func saveToken(_ token: String) throws {
        self.token = token
    }

    func deleteToken() throws {
        token = nil
        didDelete = true
    }
}

private final class AppModelURLProtocol: URLProtocol {
    static var requestHandler: ((URLRequest) throws -> (HTTPURLResponse, Data))?
    static var responseDelay: ((URLRequest) -> TimeInterval)?

    override class func canInit(with _: URLRequest) -> Bool {
        true
    }

    override class func canonicalRequest(for request: URLRequest) -> URLRequest {
        request
    }

    override func startLoading() {
        guard let handler = Self.requestHandler else {
            client?.urlProtocol(self, didFailWithError: TestError.unexpectedRequest)
            return
        }

        do {
            let (response, data) = try handler(request)
            let delay = Self.responseDelay?(request) ?? 0
            if delay > 0 {
                DispatchQueue.global().asyncAfter(deadline: .now() + delay) { [weak self] in
                    self?.deliver(response: response, data: data)
                }
                return
            }
            deliver(response: response, data: data)
        } catch {
            client?.urlProtocol(self, didFailWithError: error)
        }
    }

    private func deliver(response: HTTPURLResponse, data: Data) {
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: data)
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}

private final class LockedCounter: @unchecked Sendable {
    private let lock = NSLock()
    private var value = 0

    func increment() -> Int {
        lock.lock()
        defer { lock.unlock() }
        value += 1
        return value
    }

    var currentValue: Int {
        lock.lock()
        defer { lock.unlock() }
        return value
    }
}

@MainActor
private final class SuspendedAuthenticationSession {
    private var continuation: CheckedContinuation<URL, Error>?

    func run() async throws -> URL {
        try await withCheckedThrowingContinuation { continuation in
            self.continuation = continuation
        }
    }

    func succeed() {
        continuation?.resume(
            returning: URL(string: "slipramp://auth/callback?code=stale-grant")!
        )
        continuation = nil
    }
}

private enum TestError: LocalizedError {
    case authenticationFailed
    case unexpectedRequest
    case unexpectedState

    var errorDescription: String? {
        switch self {
        case .authenticationFailed:
            return "The identity provider rejected the sign-in."
        case .unexpectedRequest:
            return "The test received an unexpected request."
        case .unexpectedState:
            return "The connection reached an unexpected state."
        }
    }
}
