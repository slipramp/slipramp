import CryptoKit
import Foundation
import OpenAPIRuntime
import OpenAPIURLSession

struct SliprampAPIClient: @unchecked Sendable {
    let baseURL: URL

    private let token: String
    private let session: URLSession
    private let generatedClient: Client

    init(baseURL: URL, token: String = "", session: URLSession? = nil) {
        let requestSession = Self.makeSession(token: token, basedOn: session)

        self.baseURL = baseURL
        self.token = token
        self.session = requestSession
        generatedClient = Client(
            serverURL: baseURL,
            configuration: .init(dateTranscoder: SliprampDateTranscoder()),
            transport: URLSessionTransport(
                configuration: .init(session: requestSession)
            )
        )
    }

    func getClientCompatibility() async throws -> ServerCompatibility {
        let output = try await callGenerated {
            try await generatedClient.getClientCompatibility()
        }

        switch output {
        case let .ok(response):
            return try response.body.json
        case let .methodNotAllowed(response):
            throw try httpError(status: 405, problem: response.body.applicationProblemJson)
        case let .undocumented(statusCode, _):
            if statusCode == 404 {
                throw SliprampAPIError.compatibilityUnavailable
            }
            throw SliprampAPIError.http(status: statusCode, title: nil, detail: nil)
        }
    }

    func nativeOIDCLoginURL(codeChallenge: String) throws -> URL {
        let endpoint = baseURL.appendingPathComponent("api/v1/auth/native/login")
        guard var components = URLComponents(url: endpoint, resolvingAgainstBaseURL: false) else {
            throw SliprampAPIError.invalidServerURL
        }
        components.queryItems = [URLQueryItem(name: "codeChallenge", value: codeChallenge)]
        guard let url = components.url else {
            throw SliprampAPIError.invalidServerURL
        }
        return url
    }

    func exchangeNativeOIDCLogin(code: String, codeVerifier: String) async throws -> String {
        let request = Components.Schemas.NativeOIDCExchange(
            code: code,
            codeVerifier: codeVerifier
        )
        let output = try await callGenerated {
            try await generatedClient.exchangeNativeOIDCLogin(body: .json(request))
        }

        switch output {
        case let .ok(response):
            return try response.body.json.token
        case let .badRequest(response):
            throw try httpError(status: 400, problem: response.body.applicationProblemJson)
        case let .methodNotAllowed(response):
            throw try httpError(status: 405, problem: response.body.applicationProblemJson)
        case let .internalServerError(response):
            throw try httpError(status: 500, problem: response.body.applicationProblemJson)
        case let .undocumented(statusCode, _):
            throw SliprampAPIError.http(status: statusCode, title: nil, detail: nil)
        }
    }

    static func nativeOIDCAuthorizationCode(from callbackURL: URL) throws -> String {
        guard callbackURL.scheme == "slipramp",
              callbackURL.host == "auth",
              callbackURL.path == "/callback",
              let components = URLComponents(url: callbackURL, resolvingAgainstBaseURL: false)
        else {
            throw SliprampAPIError.nativeOIDCSignInFailed
        }
        if components.queryItems?.contains(where: { $0.name == "error" }) == true {
            throw SliprampAPIError.nativeOIDCSignInFailed
        }
        guard let code = components.queryItems?.first(where: { $0.name == "code" })?.value,
              !code.isEmpty
        else {
            throw SliprampAPIError.nativeOIDCSignInFailed
        }
        return code
    }

    static func normalizeServerURL(_ value: String) throws -> URL {
        let trimmed = value.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else {
            throw SliprampAPIError.invalidServerURL
        }

        let candidate = trimmed.contains("://") ? trimmed : "https://\(trimmed)"
        guard var components = URLComponents(string: candidate),
              let scheme = components.scheme?.lowercased(),
              ["http", "https"].contains(scheme),
              components.host != nil,
              components.user == nil,
              components.password == nil,
              components.query == nil,
              components.fragment == nil
        else {
            throw SliprampAPIError.invalidServerURL
        }

        components.scheme = scheme
        if components.path.count > 1 {
            components.path = components.path.replacingOccurrences(
                of: "/+$",
                with: "",
                options: .regularExpression
            )
        }

        guard let url = components.url else {
            throw SliprampAPIError.invalidServerURL
        }
        return url
    }

    static func transportSecurity(for url: URL) throws -> ServerTransportSecurity {
        switch url.scheme?.lowercased() {
        case "https":
            return .https
        case "http":
            guard let host = url.host, isPrivateHTTPHost(host) else {
                throw SliprampAPIError.publicHTTPNotAllowed
            }
            return .trustedPrivateHTTP
        default:
            throw SliprampAPIError.invalidServerURL
        }
    }

    func getInbox() async throws -> Inbox {
        let output = try await callGenerated {
            try await generatedClient.getInbox()
        }

        switch output {
        case let .ok(response):
            return try response.body.json
        case let .unauthorized(response):
            throw try httpError(status: 401, problem: response.body.applicationProblemJson)
        case let .forbidden(response):
            throw try httpError(status: 403, problem: response.body.applicationProblemJson)
        case let .methodNotAllowed(response):
            throw try httpError(status: 405, problem: response.body.applicationProblemJson)
        case let .internalServerError(response):
            throw try httpError(status: 500, problem: response.body.applicationProblemJson)
        case let .undocumented(statusCode, _):
            throw SliprampAPIError.http(status: statusCode, title: nil, detail: nil)
        }
    }

    func listTargets(
        query: String? = nil,
        limit: Int = 100,
        offset: Int = 0
    ) async throws -> TargetList {
        guard let generatedOffset = Int32(exactly: offset) else {
            throw SliprampAPIError.invalidPagination
        }

        let output = try await callGenerated {
            try await generatedClient.listTargets(
                query: .init(
                    q: query,
                    limit: limit,
                    offset: generatedOffset
                )
            )
        }

        switch output {
        case let .ok(response):
            return try response.body.json
        case let .badRequest(response):
            throw try httpError(status: 400, problem: response.body.applicationProblemJson)
        case let .unauthorized(response):
            throw try httpError(status: 401, problem: response.body.applicationProblemJson)
        case let .forbidden(response):
            throw try httpError(status: 403, problem: response.body.applicationProblemJson)
        case let .methodNotAllowed(response):
            throw try httpError(status: 405, problem: response.body.applicationProblemJson)
        case let .internalServerError(response):
            throw try httpError(status: 500, problem: response.body.applicationProblemJson)
        case let .undocumented(statusCode, _):
            throw SliprampAPIError.http(status: statusCode, title: nil, detail: nil)
        }
    }

    func getTarget(id: String) async throws -> CatalogTarget {
        let output = try await callGenerated {
            try await generatedClient.getTarget(path: .init(targetId: id))
        }

        switch output {
        case let .ok(response):
            return try response.body.json
        case let .unauthorized(response):
            throw try httpError(status: 401, problem: response.body.applicationProblemJson)
        case let .forbidden(response):
            throw try httpError(status: 403, problem: response.body.applicationProblemJson)
        case let .notFound(response):
            throw try httpError(status: 404, problem: response.body.applicationProblemJson)
        case let .methodNotAllowed(response):
            throw try httpError(status: 405, problem: response.body.applicationProblemJson)
        case let .internalServerError(response):
            throw try httpError(status: 500, problem: response.body.applicationProblemJson)
        case let .undocumented(statusCode, _):
            throw SliprampAPIError.http(status: statusCode, title: nil, detail: nil)
        }
    }

    func listRuns(limit: Int = 50, offset: Int = 0) async throws -> RunList {
        guard let generatedOffset = Int32(exactly: offset) else {
            throw SliprampAPIError.invalidPagination
        }

        let output = try await callGenerated {
            try await generatedClient.listRuns(
                query: .init(limit: limit, offset: generatedOffset)
            )
        }

        switch output {
        case let .ok(response):
            return try response.body.json
        case let .badRequest(response):
            throw try httpError(status: 400, problem: response.body.applicationProblemJson)
        case let .unauthorized(response):
            throw try httpError(status: 401, problem: response.body.applicationProblemJson)
        case let .forbidden(response):
            throw try httpError(status: 403, problem: response.body.applicationProblemJson)
        case let .methodNotAllowed(response):
            throw try httpError(status: 405, problem: response.body.applicationProblemJson)
        case let .internalServerError(response):
            throw try httpError(status: 500, problem: response.body.applicationProblemJson)
        case let .undocumented(statusCode, _):
            throw SliprampAPIError.http(status: statusCode, title: nil, detail: nil)
        }
    }

    func createRun(
        targetID: String,
        action: String,
        parameters: [String: ActionParameterValue],
        inputs: [String: String] = [:],
        idempotencyKey: String
    ) async throws -> Run {
        let request = Components.Schemas.RunCreate(
            targetId: targetID,
            action: action,
            parameters: .init(additionalProperties: parameters),
            inputs: .init(additionalProperties: inputs)
        )
        let output = try await callGenerated {
            try await generatedClient.createRun(
                headers: .init(idempotencyKey: idempotencyKey),
                body: .json(request)
            )
        }

        switch output {
        case let .created(response):
            return try response.body.json
        case let .badRequest(response):
            throw try httpError(status: 400, problem: response.body.applicationProblemJson)
        case let .unauthorized(response):
            throw try httpError(status: 401, problem: response.body.applicationProblemJson)
        case let .forbidden(response):
            throw try httpError(status: 403, problem: response.body.applicationProblemJson)
        case let .notFound(response):
            throw try httpError(status: 404, problem: response.body.applicationProblemJson)
        case let .methodNotAllowed(response):
            throw try httpError(status: 405, problem: response.body.applicationProblemJson)
        case let .conflict(response):
            throw try httpError(status: 409, problem: response.body.applicationProblemJson)
        case let .contentTooLarge(response):
            throw try httpError(status: 413, problem: response.body.applicationProblemJson)
        case let .internalServerError(response):
            throw try httpError(status: 500, problem: response.body.applicationProblemJson)
        case let .undocumented(statusCode, _):
            throw SliprampAPIError.http(status: statusCode, title: nil, detail: nil)
        }
    }

    func getRun(id: String) async throws -> Run {
        let output = try await callGenerated {
            try await generatedClient.getRun(path: .init(runId: id))
        }

        switch output {
        case let .ok(response):
            return try response.body.json
        case let .unauthorized(response):
            throw try httpError(status: 401, problem: response.body.applicationProblemJson)
        case let .forbidden(response):
            throw try httpError(status: 403, problem: response.body.applicationProblemJson)
        case let .notFound(response):
            throw try httpError(status: 404, problem: response.body.applicationProblemJson)
        case let .methodNotAllowed(response):
            throw try httpError(status: 405, problem: response.body.applicationProblemJson)
        case let .internalServerError(response):
            throw try httpError(status: 500, problem: response.body.applicationProblemJson)
        case let .undocumented(statusCode, _):
            throw SliprampAPIError.http(status: statusCode, title: nil, detail: nil)
        }
    }

    func cancelRun(id: String) async throws -> Run {
        let output = try await callGenerated {
            try await generatedClient.cancelRun(path: .init(runId: id))
        }

        switch output {
        case let .ok(response):
            return try response.body.json
        case let .unauthorized(response):
            throw try httpError(status: 401, problem: response.body.applicationProblemJson)
        case let .forbidden(response):
            throw try httpError(status: 403, problem: response.body.applicationProblemJson)
        case let .notFound(response):
            throw try httpError(status: 404, problem: response.body.applicationProblemJson)
        case let .methodNotAllowed(response):
            throw try httpError(status: 405, problem: response.body.applicationProblemJson)
        case let .conflict(response):
            throw try httpError(status: 409, problem: response.body.applicationProblemJson)
        case let .internalServerError(response):
            throw try httpError(status: 500, problem: response.body.applicationProblemJson)
        case let .undocumented(statusCode, _):
            throw SliprampAPIError.http(status: statusCode, title: nil, detail: nil)
        }
    }

    func rerunRun(
        id: String,
        mode: RunRerunMode,
        idempotencyKey: String
    ) async throws -> Run {
        let request = Components.Schemas.RunRerun(mode: mode)
        let output = try await callGenerated {
            try await generatedClient.rerunRun(
                .init(
                    path: .init(runId: id),
                    headers: .init(idempotencyKey: idempotencyKey),
                    body: .json(request)
                )
            )
        }

        switch output {
        case let .created(response):
            return try response.body.json
        case let .badRequest(response):
            throw try httpError(status: 400, problem: response.body.applicationProblemJson)
        case let .unauthorized(response):
            throw try httpError(status: 401, problem: response.body.applicationProblemJson)
        case let .forbidden(response):
            throw try httpError(status: 403, problem: response.body.applicationProblemJson)
        case let .notFound(response):
            throw try httpError(status: 404, problem: response.body.applicationProblemJson)
        case let .methodNotAllowed(response):
            throw try httpError(status: 405, problem: response.body.applicationProblemJson)
        case let .conflict(response):
            throw try httpError(status: 409, problem: response.body.applicationProblemJson)
        case let .contentTooLarge(response):
            throw try httpError(status: 413, problem: response.body.applicationProblemJson)
        case let .internalServerError(response):
            throw try httpError(status: 500, problem: response.body.applicationProblemJson)
        case let .undocumented(statusCode, _):
            throw SliprampAPIError.http(status: statusCode, title: nil, detail: nil)
        }
    }

    func decideRunReview(
        id: String,
        decision: RunReviewDecision,
        comment: String? = nil
    ) async throws -> Run {
        let request = Components.Schemas.RunReviewDecisionRequest(
            decision: decision,
            comment: comment
        )
        let output = try await callGenerated {
            try await generatedClient.decideRunReview(
                path: .init(runId: id),
                body: .json(request)
            )
        }

        switch output {
        case let .ok(response):
            return try response.body.json
        case let .badRequest(response):
            throw try httpError(status: 400, problem: response.body.applicationProblemJson)
        case let .unauthorized(response):
            throw try httpError(status: 401, problem: response.body.applicationProblemJson)
        case let .forbidden(response):
            throw try httpError(status: 403, problem: response.body.applicationProblemJson)
        case let .notFound(response):
            throw try httpError(status: 404, problem: response.body.applicationProblemJson)
        case let .methodNotAllowed(response):
            throw try httpError(status: 405, problem: response.body.applicationProblemJson)
        case let .conflict(response):
            throw try httpError(status: 409, problem: response.body.applicationProblemJson)
        case let .contentTooLarge(response):
            throw try httpError(status: 413, problem: response.body.applicationProblemJson)
        case let .internalServerError(response):
            throw try httpError(status: 500, problem: response.body.applicationProblemJson)
        case let .undocumented(statusCode, _):
            throw SliprampAPIError.http(status: statusCode, title: nil, detail: nil)
        }
    }

    func listPromotions(limit: Int = 50, offset: Int = 0) async throws -> PromotionList {
        let output = try await callGenerated {
            try await generatedClient.listPromotions(
                query: .init(limit: limit, offset: offset)
            )
        }

        switch output {
        case let .ok(response):
            return try response.body.json
        case let .badRequest(response):
            throw try httpError(status: 400, problem: response.body.applicationProblemJson)
        case let .unauthorized(response):
            throw try httpError(status: 401, problem: response.body.applicationProblemJson)
        case let .forbidden(response):
            throw try httpError(status: 403, problem: response.body.applicationProblemJson)
        case let .internalServerError(response):
            throw try httpError(status: 500, problem: response.body.applicationProblemJson)
        case let .undocumented(statusCode, _):
            throw SliprampAPIError.http(status: statusCode, title: nil, detail: nil)
        }
    }

    func getPromotion(id: String) async throws -> Promotion {
        let output = try await callGenerated {
            try await generatedClient.getPromotion(path: .init(promotionId: id))
        }

        switch output {
        case let .ok(response):
            return try response.body.json
        case let .unauthorized(response):
            throw try httpError(status: 401, problem: response.body.applicationProblemJson)
        case let .forbidden(response):
            throw try httpError(status: 403, problem: response.body.applicationProblemJson)
        case let .notFound(response):
            throw try httpError(status: 404, problem: response.body.applicationProblemJson)
        case let .internalServerError(response):
            throw try httpError(status: 500, problem: response.body.applicationProblemJson)
        case let .undocumented(statusCode, _):
            throw SliprampAPIError.http(status: statusCode, title: nil, detail: nil)
        }
    }

    func decidePromotion(
        id: String,
        decision: PromotionDecisionChoice,
        comment: String? = nil
    ) async throws -> Promotion {
        let request = Components.Schemas.PromotionDecisionUpdate(
            decision: decision,
            comment: comment
        )
        let output = try await callGenerated {
            try await generatedClient.decidePromotion(
                path: .init(promotionId: id),
                body: .json(request)
            )
        }

        switch output {
        case let .ok(response):
            return try response.body.json
        case let .badRequest(response):
            throw try httpError(status: 400, problem: response.body.applicationProblemJson)
        case let .unauthorized(response):
            throw try httpError(status: 401, problem: response.body.applicationProblemJson)
        case let .forbidden(response):
            throw try httpError(status: 403, problem: response.body.applicationProblemJson)
        case let .notFound(response):
            throw try httpError(status: 404, problem: response.body.applicationProblemJson)
        case let .conflict(response):
            throw try httpError(status: 409, problem: response.body.applicationProblemJson)
        case let .contentTooLarge(response):
            throw try httpError(status: 413, problem: response.body.applicationProblemJson)
        case let .internalServerError(response):
            throw try httpError(status: 500, problem: response.body.applicationProblemJson)
        case let .undocumented(statusCode, _):
            throw SliprampAPIError.http(status: statusCode, title: nil, detail: nil)
        }
    }

    func listRunArtifacts(runID: String) async throws -> ArtifactList {
        let output = try await callGenerated {
            try await generatedClient.listRunArtifacts(path: .init(runId: runID))
        }

        switch output {
        case let .ok(response):
            return try response.body.json
        case let .unauthorized(response):
            throw try httpError(status: 401, problem: response.body.applicationProblemJson)
        case let .forbidden(response):
            throw try httpError(status: 403, problem: response.body.applicationProblemJson)
        case let .notFound(response):
            throw try httpError(status: 404, problem: response.body.applicationProblemJson)
        case let .methodNotAllowed(response):
            throw try httpError(status: 405, problem: response.body.applicationProblemJson)
        case let .internalServerError(response):
            throw try httpError(status: 500, problem: response.body.applicationProblemJson)
        case let .undocumented(statusCode, _):
            throw SliprampAPIError.http(status: statusCode, title: nil, detail: nil)
        }
    }

    func downloadArtifactText(id: String) async throws -> String {
        let download = try await artifactDownload(id: id)
        let maximumPreviewBytes = 16 << 20
        var data = Data()
        if case let .known(length) = download.body.length {
            data.reserveCapacity(Int(min(length, Int64(maximumPreviewBytes))))
        }

        for try await chunk in download.body {
            guard data.count <= maximumPreviewBytes - chunk.count else {
                throw SliprampAPIError.invalidArtifact("The artifact exceeds the 16 MiB preview limit.")
            }
            data.append(contentsOf: chunk)
        }

        try validateArtifactDigest(data: data, responseDigest: download.digest)
        guard let text = String(data: data, encoding: .utf8) else {
            throw SliprampAPIError.invalidArtifact("The artifact is not valid UTF-8 text.")
        }
        return text
    }

    func downloadArtifact(
        id: String,
        to destination: URL,
        expectedSHA256: String? = nil
    ) async throws {
        let download = try await artifactDownload(id: id)
        let fileManager = FileManager.default
        guard !fileManager.fileExists(atPath: destination.path) else {
            throw SliprampAPIError.invalidArtifact("The download destination already exists.")
        }

        try fileManager.createDirectory(
            at: destination.deletingLastPathComponent(),
            withIntermediateDirectories: true
        )
        guard fileManager.createFile(atPath: destination.path, contents: nil) else {
            throw SliprampAPIError.invalidArtifact("The download destination could not be created.")
        }

        let file: FileHandle
        do {
            file = try FileHandle(forWritingTo: destination)
        } catch {
            try? fileManager.removeItem(at: destination)
            throw error
        }
        var hasher = SHA256()
        do {
            for try await chunk in download.body {
                let data = Data(chunk)
                try file.write(contentsOf: data)
                hasher.update(data: data)
            }
            try file.close()

            let digest = hasher.finalize().map { String(format: "%02x", $0) }.joined()
            try validateArtifactDigest(
                actualSHA256: digest,
                responseDigest: download.digest,
                expectedSHA256: expectedSHA256
            )
        } catch {
            try? file.close()
            try? fileManager.removeItem(at: destination)
            throw error
        }
    }

    private func artifactDownload(id: String) async throws -> (body: HTTPBody, digest: String?) {
        let output = try await callGenerated {
            try await generatedClient.downloadArtifact(path: .init(artifactId: id))
        }

        switch output {
        case let .ok(response):
            return (try response.body.binary, response.headers.digest)
        case let .unauthorized(response):
            throw try httpError(status: 401, problem: response.body.applicationProblemJson)
        case let .forbidden(response):
            throw try httpError(status: 403, problem: response.body.applicationProblemJson)
        case let .notFound(response):
            throw try httpError(status: 404, problem: response.body.applicationProblemJson)
        case let .methodNotAllowed(response):
            throw try httpError(status: 405, problem: response.body.applicationProblemJson)
        case let .conflict(response):
            throw try httpError(status: 409, problem: response.body.applicationProblemJson)
        case let .internalServerError(response):
            throw try httpError(status: 500, problem: response.body.applicationProblemJson)
        case let .undocumented(statusCode, _):
            throw SliprampAPIError.http(status: statusCode, title: nil, detail: nil)
        }
    }

    private func validateArtifactDigest(data: Data, responseDigest: String?) throws {
        let actual = SHA256.hash(data: data).map { String(format: "%02x", $0) }.joined()
        try validateArtifactDigest(actualSHA256: actual, responseDigest: responseDigest)
    }

    private func validateArtifactDigest(
        actualSHA256: String,
        responseDigest: String?,
        expectedSHA256: String? = nil
    ) throws {
        let headerSHA256 = responseDigest.flatMap { value -> String? in
            let prefix = "sha-256="
            guard value.lowercased().hasPrefix(prefix) else {
                return nil
            }
            return String(value.dropFirst(prefix.count)).lowercased()
        }

        if responseDigest != nil, headerSHA256 == nil {
            throw SliprampAPIError.invalidArtifact("The Server returned an unsupported Digest header.")
        }
        if let headerSHA256, headerSHA256 != actualSHA256 {
            throw SliprampAPIError.invalidArtifact("The downloaded content did not match its Server digest.")
        }
        if let expectedSHA256, expectedSHA256.lowercased() != actualSHA256 {
            throw SliprampAPIError.invalidArtifact("The downloaded content did not match its Artifact metadata.")
        }
    }

    func streamRunLog(
        id: String,
        from initialOffset: Int64 = 0
    ) -> AsyncThrowingStream<RunLogStreamUpdate, Error> {
        AsyncThrowingStream { continuation in
            let task = Task {
                var offset = initialOffset
                var hasConnected = false

                do {
                    while !Task.isCancelled {
                        do {
                            let url = try endpoint(
                                path: "api/v1/runs/\(id)/logs",
                                queryItems: [URLQueryItem(name: "offsetBytes", value: String(offset))]
                            )
                            var request = URLRequest(url: url)
                            request.httpMethod = "GET"
                            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
                            request.setValue("text/event-stream", forHTTPHeaderField: "Accept")
                            request.timeoutInterval = 60

                            let (bytes, response) = try await session.bytes(for: request)
                            guard let httpResponse = response as? HTTPURLResponse else {
                                throw SliprampAPIError.invalidResponse
                            }
                            guard httpResponse.statusCode == 200 else {
                                let data = try await collect(bytes: bytes, limit: 1 << 20)
                                let problem = try? Self.problemDecoder().decode(
                                    Components.Schemas.Problem.self,
                                    from: data
                                )
                                throw SliprampAPIError.http(
                                    status: httpResponse.statusCode,
                                    title: problem?.title,
                                    detail: problem?.detail
                                )
                            }

                            let contentType = httpResponse.value(forHTTPHeaderField: "Content-Type")
                                ?? ""
                            guard contentType
                                .lowercased()
                                .split(separator: ";", maxSplits: 1)
                                .first == "text/event-stream"
                            else {
                                throw SliprampAPIError.invalidLogStream(
                                    "The Server did not return text/event-stream."
                                )
                            }

                            hasConnected = true
                            continuation.yield(.connected(offset: offset))
                            var parser = RunLogSSEParser(offset: offset)
                            var terminal = false
                            for try await byte in bytes {
                                try Task.checkCancellation()
                                guard let update = try parser.consume(byte: byte) else {
                                    continue
                                }
                                offset = parser.offset
                                continuation.yield(update)

                                switch update {
                                case .sealed, .failed:
                                    terminal = true
                                case .connected, .chunk, .reconnecting:
                                    break
                                }
                                if terminal {
                                    break
                                }
                            }

                            if terminal {
                                continuation.finish()
                                return
                            }
                            offset = parser.offset
                        } catch is CancellationError {
                            continuation.finish()
                            return
                        } catch {
                            guard hasConnected, shouldReconnectLogStream(after: error) else {
                                throw error
                            }
                        }

                        continuation.yield(.reconnecting(offset: offset))
                        try await Task.sleep(nanoseconds: 250_000_000)
                    }
                    continuation.finish()
                } catch is CancellationError {
                    continuation.finish()
                } catch {
                    continuation.finish(throwing: error)
                }
            }

            continuation.onTermination = { @Sendable _ in
                task.cancel()
            }
        }
    }

    private func callGenerated<Response>(
        _ operation: () async throws -> Response
    ) async throws -> Response {
        do {
            return try await operation()
        } catch let error as ClientError {
            if error.response != nil {
                throw SliprampAPIError.decoding(error.causeDescription)
            }
            throw Self.mapConnectionError(error.underlyingError)
        } catch {
            throw Self.mapConnectionError(error)
        }
    }

    private func httpError(
        status: Int,
        problem: @autoclosure () throws -> Components.Schemas.Problem
    ) rethrows -> SliprampAPIError {
        let problem = try problem()
        return .http(status: status, title: problem.title, detail: problem.detail)
    }

    private func endpoint(path: String, queryItems: [URLQueryItem]) throws -> URL {
        guard var components = URLComponents(url: baseURL, resolvingAgainstBaseURL: false) else {
            throw SliprampAPIError.invalidServerURL
        }

        let basePath = components.path.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
        let endpointPath = path.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
        components.path = "/" + [basePath, endpointPath].filter { !$0.isEmpty }.joined(separator: "/")
        components.queryItems = queryItems.isEmpty ? nil : queryItems

        guard let url = components.url else {
            throw SliprampAPIError.invalidServerURL
        }
        return url
    }

    private static func problemDecoder() -> JSONDecoder {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return decoder
    }

    private static func makeSession(token: String, basedOn session: URLSession?) -> URLSession {
        let configuration = session?.configuration ?? .ephemeral
        var headers = configuration.httpAdditionalHeaders ?? [:]
        if !token.isEmpty {
            headers["Authorization"] = "Bearer \(token)"
        }
        configuration.httpAdditionalHeaders = headers
        configuration.waitsForConnectivity = true
        configuration.timeoutIntervalForRequest = 30
        configuration.timeoutIntervalForResource = 60
        return URLSession(configuration: configuration)
    }

    private static func isPrivateHTTPHost(_ host: String) -> Bool {
        let normalized = host
            .lowercased()
            .trimmingCharacters(in: CharacterSet(charactersIn: "[]"))

        if normalized == "localhost" || normalized.hasSuffix(".localhost") ||
            normalized.hasSuffix(".local")
        {
            return true
        }
        if normalized.contains(":") {
            return normalized == "::1" || normalized.hasPrefix("fc") ||
                normalized.hasPrefix("fd") || normalized.hasPrefix("fe8") ||
                normalized.hasPrefix("fe9") || normalized.hasPrefix("fea") ||
                normalized.hasPrefix("feb")
        }

        let octets = normalized.split(separator: ".", omittingEmptySubsequences: false)
        if octets.count == 4,
           let values = try? octets.map({ octet -> UInt8 in
               guard let value = UInt8(octet) else {
                   throw SliprampAPIError.invalidServerURL
               }
               return value
           })
        {
            return values[0] == 10 ||
                values[0] == 127 ||
                (values[0] == 169 && values[1] == 254) ||
                (values[0] == 172 && (16 ... 31).contains(values[1])) ||
                (values[0] == 192 && values[1] == 168) ||
                (values[0] == 100 && (64 ... 127).contains(values[1]))
        }

        return !normalized.contains(".")
    }

    private static func mapConnectionError(_ error: Error) -> Error {
        let underlying = error as NSError
        guard underlying.domain == NSURLErrorDomain else {
            return error
        }
        let code = URLError.Code(rawValue: underlying.code)

        switch code {
        case .serverCertificateHasBadDate,
             .serverCertificateUntrusted,
             .serverCertificateHasUnknownRoot,
             .serverCertificateNotYetValid,
             .secureConnectionFailed,
             .clientCertificateRejected,
             .clientCertificateRequired:
            return SliprampAPIError.connectionFailed(.certificate)
        case .appTransportSecurityRequiresSecureConnection:
            return SliprampAPIError.connectionFailed(.insecureTransport)
        case .cannotFindHost, .dnsLookupFailed:
            return SliprampAPIError.connectionFailed(.serverNotFound)
        case .cannotConnectToHost,
             .networkConnectionLost,
             .notConnectedToInternet,
             .timedOut:
            return SliprampAPIError.connectionFailed(.unreachable)
        default:
            return error
        }
    }

    private func collect(bytes: URLSession.AsyncBytes, limit: Int) async throws -> Data {
        var data = Data()
        data.reserveCapacity(min(limit, 4096))
        for try await byte in bytes {
            guard data.count < limit else {
                break
            }
            data.append(byte)
        }
        return data
    }

    private func shouldReconnectLogStream(after error: Error) -> Bool {
        if let apiError = error as? SliprampAPIError {
            if case let .http(status, _, _) = apiError {
                return status == 408 || status == 429 || status >= 500
            }
            return false
        }
        return true
    }
}

private struct SliprampDateTranscoder: DateTranscoder {
    func encode(_ date: Date) throws -> String {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter.string(from: date)
    }

    func decode(_ dateString: String) throws -> Date {
        let fractional = ISO8601DateFormatter()
        fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let date = fractional.date(from: dateString) {
            return date
        }

        let standard = ISO8601DateFormatter()
        standard.formatOptions = [.withInternetDateTime]
        if let date = standard.date(from: dateString) {
            return date
        }

        throw DecodingError.dataCorrupted(
            .init(codingPath: [], debugDescription: "Expected an RFC 3339 timestamp.")
        )
    }
}

enum SliprampAPIError: LocalizedError, Equatable {
    case invalidServerURL
    case publicHTTPNotAllowed
    case trustedPrivateHTTPConfirmationRequired
    case invalidPagination
    case invalidResponse
    case compatibilityUnavailable
    case incompatibleProductAPIContract(server: Int)
    case apiKeyAuthenticationUnavailable
    case nativeOIDCAuthenticationUnavailable
    case nativeOIDCSignInFailed
    case connectionFailed(ServerConnectionFailure)
    case http(status: Int, title: String?, detail: String?)
    case decoding(String)
    case invalidArtifact(String)
    case invalidLogStream(String)

    var errorDescription: String? {
        switch self {
        case .invalidServerURL:
            return "Enter a valid HTTP or HTTPS Slipramp Server URL."
        case .publicHTTPNotAllowed:
            return "Use HTTPS for this Server. HTTP is available only for an explicitly trusted private-network address."
        case .trustedPrivateHTTPConfirmationRequired:
            return "Confirm this private HTTP Server before connecting."
        case .invalidPagination:
            return "The requested page is outside the supported range."
        case .invalidResponse:
            return "The Slipramp Server returned an invalid response."
        case .compatibilityUnavailable:
            return "This Server does not support iOS compatibility checks. Update the Slipramp Server and try again."
        case let .incompatibleProductAPIContract(server):
            if server < ClientCompatibilityPolicy.productAPIContract {
                return "This app requires a newer Slipramp Server."
            }
            return "This Server requires a newer version of Slipramp for iOS."
        case .apiKeyAuthenticationUnavailable:
            return "This Server does not offer API-key authentication for the iOS app."
        case .nativeOIDCAuthenticationUnavailable:
            return "This Server does not offer native OIDC sign-in."
        case .nativeOIDCSignInFailed:
            return "Sign in did not complete. Try again."
        case let .connectionFailed(failure):
            return failure.message
        case let .http(status, title, detail):
            return detail ?? title ?? "The Slipramp Server returned HTTP \(status)."
        case let .decoding(detail):
            return "The Slipramp response did not match this app version: \(detail)"
        case let .invalidArtifact(detail):
            return "The Slipramp artifact is invalid: \(detail)"
        case let .invalidLogStream(detail):
            return "The Slipramp log stream is invalid: \(detail)"
        }
    }
}

enum ServerTransportSecurity: Equatable {
    case https
    case trustedPrivateHTTP
}

enum ServerConnectionFailure: Equatable {
    case certificate
    case insecureTransport
    case serverNotFound
    case unreachable

    var message: String {
        switch self {
        case .certificate:
            return "The Server’s TLS certificate is not trusted for this address. Use a system-trusted certificate with the correct hostname."
        case .insecureTransport:
            return "iOS blocked this insecure connection. Use HTTPS, or explicitly trust a private HTTP Server from the connection screen."
        case .serverNotFound:
            return "The Server name could not be resolved. Check the address and your private-network connection."
        case .unreachable:
            return "The Server could not be reached. Check the address and that its local network or VPN is connected."
        }
    }
}

struct NativeOIDCPKCE: Equatable {
    let verifier: String
    let challenge: String

    static func make() -> NativeOIDCPKCE {
        var generator = SystemRandomNumberGenerator()
        let bytes = (0 ..< 32).map { _ in UInt8.random(in: .min ... .max, using: &generator) }
        let verifier = Data(bytes).base64URLEncodedString()
        return NativeOIDCPKCE(
            verifier: verifier,
            challenge: Data(SHA256.hash(data: Data(verifier.utf8))).base64URLEncodedString()
        )
    }
}

private extension Data {
    func base64URLEncodedString() -> String {
        base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }
}
