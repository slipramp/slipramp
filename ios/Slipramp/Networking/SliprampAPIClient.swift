import Foundation
import OpenAPIRuntime
import OpenAPIURLSession

struct SliprampAPIClient: @unchecked Sendable {
    let baseURL: URL

    private let token: String
    private let session: URLSession
    private let generatedClient: Client

    init(baseURL: URL, token: String, session: URLSession? = nil) {
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
        case let .internalServerError(response):
            throw try httpError(status: 500, problem: response.body.applicationProblemJson)
        case let .undocumented(statusCode, _):
            throw SliprampAPIError.http(status: statusCode, title: nil, detail: nil)
        }
    }

    func downloadArtifactText(id: String) async throws -> String {
        let output = try await callGenerated {
            try await generatedClient.downloadArtifact(path: .init(artifactId: id))
        }

        switch output {
        case let .ok(response):
            let body = try response.body.binary
            let maximumPreviewBytes = 16 << 20
            if case let .known(length) = body.length, length > maximumPreviewBytes {
                throw SliprampAPIError.invalidArtifact("The artifact exceeds the 16 MiB preview limit.")
            }
            let bytes = try await [UInt8](collecting: body, upTo: maximumPreviewBytes)
            guard let text = String(bytes: bytes, encoding: .utf8) else {
                throw SliprampAPIError.invalidArtifact("The artifact is not valid UTF-8 text.")
            }
            return text
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
                                case .chunk, .reconnecting:
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
            throw error.underlyingError
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
        headers["Authorization"] = "Bearer \(token)"
        configuration.httpAdditionalHeaders = headers
        configuration.waitsForConnectivity = true
        configuration.timeoutIntervalForRequest = 30
        configuration.timeoutIntervalForResource = 60
        return URLSession(configuration: configuration)
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
    case invalidPagination
    case invalidResponse
    case http(status: Int, title: String?, detail: String?)
    case decoding(String)
    case invalidArtifact(String)
    case invalidLogStream(String)

    var errorDescription: String? {
        switch self {
        case .invalidServerURL:
            return "Enter a valid HTTP or HTTPS Slipramp Server URL."
        case .invalidPagination:
            return "The requested Run page is outside the supported range."
        case .invalidResponse:
            return "The Slipramp Server returned an invalid response."
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
