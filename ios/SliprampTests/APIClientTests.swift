import CryptoKit
import Foundation
import XCTest
@testable import SliprampCore

final class APIClientTests: XCTestCase {
    override func tearDown() {
        StubURLProtocol.requestHandler = nil
        super.tearDown()
    }

    func testCompatibilityCheckIsPublicAndEnforcesTheExactContract() async throws {
        let session = makeSession()
        let client = SliprampAPIClient(
            baseURL: URL(string: "https://slipramp.example")!,
            session: session
        )

        StubURLProtocol.requestHandler = { request in
            XCTAssertEqual(request.url?.path, "/api/v1/client-compatibility")
            XCTAssertNil(request.value(forHTTPHeaderField: "Authorization"))

            let body = Data(
                """
                {
                  "serverVersion": "test-version",
                  "productApiContract": 1,
                  "authenticationMethods": ["api-key"]
                }
                """.utf8
            )
            return (self.response(for: request, status: 200), body)
        }

        let compatibility = try await client.getClientCompatibility()
        XCTAssertEqual(compatibility.serverVersion, "test-version")
        XCTAssertNoThrow(try ClientCompatibilityPolicy.validate(compatibility))

        let incompatible = ServerCompatibility(
            serverVersion: "future-version",
            productApiContract: 2,
            authenticationMethods: ["api-key"]
        )
        XCTAssertThrowsError(try ClientCompatibilityPolicy.validate(incompatible)) { error in
            XCTAssertEqual(
                error as? SliprampAPIError,
                .incompatibleProductAPIContract(server: 2)
            )
        }
    }

    func testNativeOIDCBuildsPKCELoginAndExchangesCallbackGrant() async throws {
        let session = makeSession()
        let client = SliprampAPIClient(
            baseURL: URL(string: "https://slipramp.example/base")!,
            session: session
        )
        let pkce = NativeOIDCPKCE.make()
        XCTAssertEqual(pkce.verifier.count, 43)
        XCTAssertEqual(pkce.challenge.count, 43)

        let loginURL = try client.nativeOIDCLoginURL(codeChallenge: pkce.challenge)
        let loginComponents = try XCTUnwrap(
            URLComponents(url: loginURL, resolvingAgainstBaseURL: false)
        )
        XCTAssertEqual(loginComponents.path, "/base/api/v1/auth/native/login")
        XCTAssertEqual(
            loginComponents.queryItems,
            [URLQueryItem(name: "codeChallenge", value: pkce.challenge)]
        )
        let callback = URL(string: "slipramp://auth/callback?code=one-time-grant")!
        XCTAssertEqual(
            try SliprampAPIClient.nativeOIDCAuthorizationCode(from: callback),
            "one-time-grant"
        )

        StubURLProtocol.requestHandler = { request in
            XCTAssertEqual(request.httpMethod, "POST")
            XCTAssertEqual(request.url?.path, "/base/api/v1/auth/native/exchange")
            XCTAssertNil(request.value(forHTTPHeaderField: "Authorization"))
            let body = try self.requestBody(request)
            let payload = try XCTUnwrap(
                JSONSerialization.jsonObject(with: body) as? [String: String]
            )
            XCTAssertEqual(payload["code"], "one-time-grant")
            XCTAssertEqual(payload["codeVerifier"], pkce.verifier)

            let response = Data(
                """
                {
                  "token": "native-session-token",
                  "expiresAt": "2026-10-31T00:00:00Z"
                }
                """.utf8
            )
            return (self.response(for: request, status: 200), response)
        }

        let token = try await client.exchangeNativeOIDCLogin(
            code: "one-time-grant",
            codeVerifier: pkce.verifier
        )
        XCTAssertEqual(token, "native-session-token")
    }

    func testInboxUsesBearerCredentialAndDecodesProjection() async throws {
        let session = makeSession()
        let client = SliprampAPIClient(
            baseURL: URL(string: "https://slipramp.example/base")!,
            token: "slipramp_key_secret",
            session: session
        )

        StubURLProtocol.requestHandler = { request in
            XCTAssertEqual(request.url?.absoluteString, "https://slipramp.example/base/api/v1/inbox")
            XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer slipramp_key_secret")

            let body = #"""
            {
              "generatedAt": "2026-10-30T12:34:56.123Z",
              "capabilities": { "acknowledgeProblems": true },
              "decisions": [],
              "problems": [],
              "activity": { "queued": 1, "starting": 2, "running": 3 }
            }
            """#.data(using: .utf8)!
            return (self.response(for: request, status: 200), body)
        }

        let inbox = try await client.getInbox()
        XCTAssertEqual(inbox.activity.total, 6)
        XCTAssertTrue(inbox.capabilities.acknowledgeProblems)
    }

    func testRunListDecodesTheGeneratedModel() async throws {
        let session = makeSession()
        let client = SliprampAPIClient(
            baseURL: URL(string: "https://slipramp.example")!,
            token: "slipramp_key_secret",
            session: session
        )
        let runID = UUID(uuidString: "19d265a2-c116-4e8f-b547-a824a9039574")!

        StubURLProtocol.requestHandler = { request in
            XCTAssertEqual(request.url?.query, "limit=50&offset=0")
            let run = self.runBody(
                id: runID.uuidString.lowercased(),
                phase: "finished",
                outcome: "succeeded",
                reviewState: "not-required"
            )
            let body = Data(
                "{\"items\":[\(String(decoding: run, as: UTF8.self))],\"limit\":50,\"offset\":0,\"total\":1}"
                    .utf8
            )
            return (self.response(for: request, status: 200), body)
        }

        let list = try await client.listRuns()
        XCTAssertEqual(list.total, 1)
        XCTAssertEqual(list.items.first?.id, runID.uuidString.lowercased())
        XCTAssertEqual(list.items.first?.displayAction, "Sync homelab")
        XCTAssertEqual(list.items.first?.statusLabel, "Succeeded")
        XCTAssertEqual(list.items.first?.commit.shortSHA, "01234567")
    }

    func testTargetListSendsSearchAndDecodesSummaries() async throws {
        let session = makeSession()
        let client = SliprampAPIClient(
            baseURL: URL(string: "https://slipramp.example")!,
            token: "slipramp_key_secret",
            session: session
        )

        StubURLProtocol.requestHandler = { request in
            let query = try XCTUnwrap(URLComponents(url: request.url!, resolvingAgainstBaseURL: false))
            XCTAssertEqual(
                query.queryItems,
                [
                    URLQueryItem(name: "q", value: "sync"),
                    URLQueryItem(name: "limit", value: "100"),
                    URLQueryItem(name: "offset", value: "0"),
                ]
            )
            return (self.response(for: request, status: 200), self.targetListBody())
        }

        let list = try await client.listTargets(query: "sync")
        XCTAssertEqual(list.total, 1)
        XCTAssertEqual(list.items.first?.displayName, "Homelab")
        XCTAssertEqual(list.items.first?.actions, ["preview", "sync"])
        XCTAssertEqual(list.items.first?.reconciliation.comparison, .matched)
    }

    func testCreateRunSendsTypedParametersAndIdempotencyKey() async throws {
        let session = makeSession()
        let client = SliprampAPIClient(
            baseURL: URL(string: "https://slipramp.example")!,
            token: "slipramp_key_secret",
            session: session
        )
        let runID = "531a9f8c-4341-455c-b87c-690fb8f6e37b"

        StubURLProtocol.requestHandler = { request in
            XCTAssertEqual(request.httpMethod, "POST")
            XCTAssertEqual(request.url?.path, "/api/v1/runs")
            XCTAssertEqual(request.value(forHTTPHeaderField: "Idempotency-Key"), "start-action-key")

            let requestBody = try self.requestBody(request)
            let payload = try XCTUnwrap(
                JSONSerialization.jsonObject(with: requestBody) as? [String: Any]
            )
            XCTAssertEqual(payload["targetId"] as? String, "929e7e9d-102a-43d0-9ca6-fc4d53dfbb14")
            XCTAssertEqual(payload["action"] as? String, "preview")
            XCTAssertEqual(payload["inputs"] as? [String: String], [:])

            let parameters = try XCTUnwrap(payload["parameters"] as? [String: Any])
            XCTAssertEqual(parameters["environment"] as? String, "production")
            XCTAssertEqual(parameters["refresh"] as? Bool, true)
            XCTAssertEqual(parameters["parallelism"] as? Double, 4)

            return (
                self.response(for: request, status: 201),
                self.runBody(id: runID, phase: "queued", reviewState: "not-required")
            )
        }

        let run = try await client.createRun(
            targetID: "929e7e9d-102a-43d0-9ca6-fc4d53dfbb14",
            action: "preview",
            parameters: [
                "environment": .case1("production"),
                "refresh": .case2(true),
                "parallelism": .case3(4),
            ],
            idempotencyKey: "start-action-key"
        )
        XCTAssertEqual(run.id, runID)
    }

    func testCreateRunBindsArtifactInput() async throws {
        let session = makeSession()
        let client = SliprampAPIClient(
            baseURL: URL(string: "https://slipramp.example")!,
            token: "slipramp_key_secret",
            session: session
        )
        let artifactID = "2a76876f-bf1b-4851-b401-16c68608962c"

        StubURLProtocol.requestHandler = { request in
            let requestBody = try self.requestBody(request)
            let payload = try XCTUnwrap(
                JSONSerialization.jsonObject(with: requestBody) as? [String: Any]
            )
            XCTAssertEqual(payload["targetId"] as? String, "929e7e9d-102a-43d0-9ca6-fc4d53dfbb14")
            XCTAssertEqual(payload["action"] as? String, "apply")
            XCTAssertEqual(payload["inputs"] as? [String: String], ["plan": artifactID])
            XCTAssertEqual(payload["parameters"] as? [String: String], [:])

            return (
                self.response(for: request, status: 201),
                self.runBody(
                    id: "531a9f8c-4341-455c-b87c-690fb8f6e37b",
                    phase: "queued",
                    reviewState: "not-required"
                )
            )
        }

        _ = try await client.createRun(
            targetID: "929e7e9d-102a-43d0-9ca6-fc4d53dfbb14",
            action: "apply",
            parameters: [:],
            inputs: ["plan": artifactID],
            idempotencyKey: "apply-artifact-key"
        )
    }

    func testCreateRunMapsContentTooLargeProblem() async throws {
        let session = makeSession()
        let client = SliprampAPIClient(
            baseURL: URL(string: "https://slipramp.example")!,
            token: "slipramp_key_secret",
            session: session
        )

        StubURLProtocol.requestHandler = { request in
            let body = Data(
                """
                {
                  "type": "about:blank",
                  "title": "Request too large",
                  "status": 413,
                  "detail": "The request body is too large."
                }
                """.utf8
            )
            return (self.response(for: request, status: 413), body)
        }

        do {
            _ = try await client.createRun(
                targetID: "929e7e9d-102a-43d0-9ca6-fc4d53dfbb14",
                action: "apply",
                parameters: [:],
                idempotencyKey: "large-request-key"
            )
            XCTFail("Expected the request to fail")
        } catch let error as SliprampAPIError {
            XCTAssertEqual(
                error,
                .http(
                    status: 413,
                    title: "Request too large",
                    detail: "The request body is too large."
                )
            )
        }
    }

    func testActionParameterDecoderValidatesAndConvertsValues() throws {
        let declarations: [String: ActionParameter] = [
            "environment": .init(
                title: "Environment",
                _type: ._enum,
                required: true,
                values: ["staging", "production"]
            ),
            "refresh": .init(_type: .boolean),
            "parallelism": .init(_type: .number),
            "note": .init(_type: .string),
        ]

        let decoded = try ActionParameterDecoder.decode(
            values: [
                "environment": "production",
                "refresh": "false",
                "parallelism": "3.5",
                "note": "operator requested",
            ],
            declarations: declarations
        )

        XCTAssertEqual(decoded["environment"], .case1("production"))
        XCTAssertEqual(decoded["refresh"], .case2(false))
        XCTAssertEqual(decoded["parallelism"], .case3(3.5))
        XCTAssertEqual(decoded["note"], .case1("operator requested"))

        XCTAssertThrowsError(
            try ActionParameterDecoder.decode(values: [:], declarations: declarations)
        ) { error in
            XCTAssertEqual(error as? ActionParameterValidationError, .required("Environment"))
        }
    }

    func testReviewDecisionSendsCommentAndReturnsUpdatedRun() async throws {
        let session = makeSession()
        let client = SliprampAPIClient(
            baseURL: URL(string: "https://slipramp.example")!,
            token: "slipramp_key_secret",
            session: session
        )
        let runID = "19d265a2-c116-4e8f-b547-a824a9039574"

        StubURLProtocol.requestHandler = { request in
            XCTAssertEqual(request.httpMethod, "POST")
            XCTAssertEqual(request.url?.path, "/api/v1/runs/\(runID)/review")
            XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer slipramp_key_secret")

            let requestBody = try self.requestBody(request)
            let payload = try XCTUnwrap(
                JSONSerialization.jsonObject(with: requestBody) as? [String: String]
            )
            XCTAssertEqual(payload["decision"], "approved")
            XCTAssertEqual(payload["comment"], "Looks safe to apply.")

            return (
                self.response(for: request, status: 200),
                self.runBody(id: runID, phase: "queued", reviewState: "approved")
            )
        }

        let run = try await client.decideRunReview(
            id: runID,
            decision: .approved,
            comment: "Looks safe to apply."
        )
        XCTAssertEqual(run.review.state, .approved)
        XCTAssertFalse(run.canDecideReview)
        XCTAssertTrue(run.canCancel)
    }

    func testCancelRunPostsWithoutBodyAndReturnsUpdatedRun() async throws {
        let session = makeSession()
        let client = SliprampAPIClient(
            baseURL: URL(string: "https://slipramp.example")!,
            token: "slipramp_key_secret",
            session: session
        )
        let runID = "19d265a2-c116-4e8f-b547-a824a9039574"

        StubURLProtocol.requestHandler = { request in
            XCTAssertEqual(request.httpMethod, "POST")
            XCTAssertEqual(request.url?.path, "/api/v1/runs/\(runID)/cancel")
            XCTAssertNil(request.httpBody)

            return (
                self.response(for: request, status: 200),
                self.runBody(
                    id: runID,
                    phase: "finished",
                    outcome: "canceled",
                    reviewState: "not-required"
                )
            )
        }

        let run = try await client.cancelRun(id: runID)
        XCTAssertEqual(run.phase, .finished)
        XCTAssertEqual(run.outcome, .canceled)
        XCTAssertFalse(run.canCancel)
    }

    func testRerunRunSendsModeAndIdempotencyKey() async throws {
        let session = makeSession()
        let client = SliprampAPIClient(
            baseURL: URL(string: "https://slipramp.example")!,
            token: "slipramp_key_secret",
            session: session
        )
        let runID = "19d265a2-c116-4e8f-b547-a824a9039574"
        let rerunID = "531a9f8c-4341-455c-b87c-690fb8f6e37b"

        StubURLProtocol.requestHandler = { request in
            XCTAssertEqual(request.httpMethod, "POST")
            XCTAssertEqual(request.url?.path, "/api/v1/runs/\(runID)/rerun")
            XCTAssertEqual(request.value(forHTTPHeaderField: "Idempotency-Key"), "rerun-key-latest")

            let requestBody = try self.requestBody(request)
            let payload = try XCTUnwrap(
                JSONSerialization.jsonObject(with: requestBody) as? [String: String]
            )
            XCTAssertEqual(payload["mode"], "latest")

            return (
                self.response(for: request, status: 201),
                self.runBody(
                    id: rerunID,
                    phase: "queued",
                    reviewState: "not-required",
                    rerunOfRunID: runID,
                    rerunMode: "latest"
                )
            )
        }

        let run = try await client.rerunRun(
            id: runID,
            mode: .latest,
            idempotencyKey: "rerun-key-latest"
        )
        XCTAssertEqual(run.id, rerunID)
        XCTAssertEqual(run.rerunOfRunId, runID)
        XCTAssertEqual(run.rerunMode, .latest)
    }

    func testPromotionDetailDecodesPreparedReviewContext() async throws {
        let session = makeSession()
        let client = SliprampAPIClient(
            baseURL: URL(string: "https://slipramp.example")!,
            token: "slipramp_key_secret",
            session: session
        )
        let promotionID = "45354aa2-a3e1-4b4c-9b2c-a52f46e72cc2"

        StubURLProtocol.requestHandler = { request in
            XCTAssertEqual(request.httpMethod, "GET")
            XCTAssertEqual(request.url?.path, "/api/v1/promotions/\(promotionID)")
            return (
                self.response(for: request, status: 200),
                self.promotionBody(id: promotionID, status: "pending")
            )
        }

        let promotion = try await client.getPromotion(id: promotionID)
        XCTAssertEqual(promotion.externalPromotionId, "promote-production-42")
        XCTAssertEqual(promotion.stageSnapshot.environment, "production")
        XCTAssertEqual(promotion.selectedFreight?.name, "freight-42")
        XCTAssertEqual(promotion.statusLabel, "Pending")
        XCTAssertTrue(promotion.canDecide)
    }

    func testPromotionListUsesGeneratedOperationAndDecodesHistory() async throws {
        let session = makeSession()
        let client = SliprampAPIClient(
            baseURL: URL(string: "https://slipramp.example")!,
            token: "slipramp_key_secret",
            session: session
        )
        let promotionID = "45354aa2-a3e1-4b4c-9b2c-a52f46e72cc2"

        StubURLProtocol.requestHandler = { request in
            XCTAssertEqual(request.httpMethod, "GET")
            XCTAssertEqual(request.url?.path, "/api/v1/promotions")
            XCTAssertEqual(request.url?.query, "limit=50&offset=0")
            XCTAssertEqual(
                request.value(forHTTPHeaderField: "Authorization"),
                "Bearer slipramp_key_secret"
            )

            let promotion = self.promotionBody(id: promotionID, status: "pending")
            let body = Data(
                "{\"items\":[\(String(decoding: promotion, as: UTF8.self))],\"total\":1,\"limit\":50,\"offset\":0}"
                    .utf8
            )
            return (self.response(for: request, status: 200), body)
        }

        let list = try await client.listPromotions()
        XCTAssertEqual(list.total, 1)
        XCTAssertEqual(list.items.first?.id, promotionID)
        XCTAssertEqual(list.items.first?.stageSnapshot.application, "slipramp")
        XCTAssertEqual(list.items.first?.statusLabel, "Pending")
    }

    func testPromotionDecisionSendsCommentAndReturnsImmutableAttribution() async throws {
        let session = makeSession()
        let client = SliprampAPIClient(
            baseURL: URL(string: "https://slipramp.example")!,
            token: "slipramp_key_secret",
            session: session
        )
        let promotionID = "45354aa2-a3e1-4b4c-9b2c-a52f46e72cc2"

        StubURLProtocol.requestHandler = { request in
            XCTAssertEqual(request.httpMethod, "PUT")
            XCTAssertEqual(request.url?.path, "/api/v1/promotions/\(promotionID)/decision")

            let requestBody = try self.requestBody(request)
            let payload = try XCTUnwrap(
                JSONSerialization.jsonObject(with: requestBody) as? [String: String]
            )
            XCTAssertEqual(payload["decision"], "approved")
            XCTAssertEqual(payload["comment"], "Diffs match the change request.")

            return (
                self.response(for: request, status: 200),
                self.promotionBody(id: promotionID, status: "approved", decided: true)
            )
        }

        let promotion = try await client.decidePromotion(
            id: promotionID,
            decision: .approved,
            comment: "Diffs match the change request."
        )
        XCTAssertEqual(promotion.decision?.decision, .approved)
        XCTAssertEqual(promotion.decision?.decidedBy.displayName, "Ian Operator")
        XCTAssertFalse(promotion.canDecide)
    }

    func testArtifactDownloadUsesGeneratedBinaryOperation() async throws {
        let session = makeSession()
        let client = SliprampAPIClient(
            baseURL: URL(string: "https://slipramp.example")!,
            token: "slipramp_key_secret",
            session: session
        )
        let artifactID = "2a76876f-bf1b-4851-b401-16c68608962c"

        let contents = Data("@@ -1 +1 @@\n-old\n+new\n".utf8)
        let digest = SHA256.hash(data: contents).map { String(format: "%02x", $0) }.joined()

        StubURLProtocol.requestHandler = { request in
            XCTAssertEqual(request.httpMethod, "GET")
            XCTAssertEqual(request.url?.path, "/api/v1/artifacts/\(artifactID)/download")
            XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer slipramp_key_secret")
            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: 200,
                httpVersion: "HTTP/1.1",
                headerFields: [
                    "Content-Type": "application/octet-stream",
                    "Digest": "sha-256=\(digest)",
                ]
            )!
            return (response, contents)
        }

        let text = try await client.downloadArtifactText(id: artifactID)
        XCTAssertEqual(text, "@@ -1 +1 @@\n-old\n+new\n")
    }

    func testRunArtifactListUsesGeneratedOperation() async throws {
        let session = makeSession()
        let client = SliprampAPIClient(
            baseURL: URL(string: "https://slipramp.example")!,
            token: "slipramp_key_secret",
            session: session
        )
        let runID = "19d265a2-c116-4e8f-b547-a824a9039574"
        let artifactID = "2a76876f-bf1b-4851-b401-16c68608962c"

        StubURLProtocol.requestHandler = { request in
            XCTAssertEqual(request.httpMethod, "GET")
            XCTAssertEqual(request.url?.path, "/api/v1/runs/\(runID)/artifacts")
            let body = Data(
                """
                {
                  "items": [
                    {
                      "id": "\(artifactID)",
                      "runId": "\(runID)",
                      "name": "plan",
                      "description": "Reviewed infrastructure plan.",
                      "kind": "output",
                      "state": "sealed",
                      "mediaType": "application/json",
                      "sizeBytes": 2048,
                      "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
                      "sensitive": true,
                      "retentionState": "retained",
                      "provenance": {
                        "producerRunId": "\(runID)",
                        "repositoryId": "ff281fe6-d60b-4312-840c-df2714f89717",
                        "target": "homelab",
                        "action": "plan",
                        "commit": { "sha": "0123456789abcdef0123456789abcdef01234567" },
                        "catalogRevisionId": "ced99dba-bb0b-4a8a-a9be-a5ef9875b945"
                      },
                      "createdAt": "2026-10-30T12:34:56Z",
                      "sealedAt": "2026-10-30T12:35:10Z"
                    }
                  ]
                }
                """.utf8
            )
            return (self.response(for: request, status: 200), body)
        }

        let list = try await client.listRunArtifacts(runID: runID)
        XCTAssertEqual(list.items.count, 1)
        XCTAssertEqual(list.items[0].id, artifactID)
        XCTAssertEqual(list.items[0].name, "plan")
        XCTAssertEqual(list.items[0].statusLabel, "Sealed")
        XCTAssertTrue(list.items[0].isDownloadable)
    }

    func testArtifactDownloadStreamsVerifiedFile() async throws {
        let session = makeSession()
        let client = SliprampAPIClient(
            baseURL: URL(string: "https://slipramp.example")!,
            token: "slipramp_key_secret",
            session: session
        )
        let artifactID = "2a76876f-bf1b-4851-b401-16c68608962c"
        let contents = Data("immutable plan bytes".utf8)
        let digest = SHA256.hash(data: contents).map { String(format: "%02x", $0) }.joined()

        StubURLProtocol.requestHandler = { request in
            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: 200,
                httpVersion: "HTTP/1.1",
                headerFields: [
                    "Content-Type": "application/octet-stream",
                    "Digest": "sha-256=\(digest)",
                ]
            )!
            return (response, contents)
        }

        let destination = FileManager.default.temporaryDirectory
            .appendingPathComponent("slipramp-artifact-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: destination) }

        try await client.downloadArtifact(
            id: artifactID,
            to: destination,
            expectedSHA256: digest
        )
        XCTAssertEqual(try Data(contentsOf: destination), contents)

        let rejectedDestination = destination.appendingPathExtension("rejected")
        defer { try? FileManager.default.removeItem(at: rejectedDestination) }
        do {
            try await client.downloadArtifact(
                id: artifactID,
                to: rejectedDestination,
                expectedSHA256: String(repeating: "0", count: 64)
            )
            XCTFail("Expected the metadata digest mismatch to fail")
        } catch let error as SliprampAPIError {
            XCTAssertEqual(
                error,
                .invalidArtifact("The downloaded content did not match its Artifact metadata.")
            )
            XCTAssertFalse(FileManager.default.fileExists(atPath: rejectedDestination.path))
        }
    }

    func testRunLogStreamReportsConnectionBeforeTerminalEvent() async throws {
        let session = makeSession()
        let client = SliprampAPIClient(
            baseURL: URL(string: "https://slipramp.example")!,
            token: "slipramp_key_secret",
            session: session
        )
        let runID = "19d265a2-c116-4e8f-b547-a824a9039574"

        StubURLProtocol.requestHandler = { request in
            XCTAssertEqual(request.httpMethod, "GET")
            XCTAssertEqual(request.url?.path, "/api/v1/runs/\(runID)/logs")
            XCTAssertEqual(request.url?.query, "offsetBytes=0")
            XCTAssertEqual(request.value(forHTTPHeaderField: "Accept"), "text/event-stream")

            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: 200,
                httpVersion: "HTTP/1.1",
                headerFields: ["Content-Type": "text/event-stream"]
            )!
            return (response, Data("id: 0\nevent: sealed\ndata: {}\n\n".utf8))
        }

        var iterator = client.streamRunLog(id: runID).makeAsyncIterator()
        let connected = try await iterator.next()
        let sealed = try await iterator.next()
        let finished = try await iterator.next()
        XCTAssertEqual(connected, .connected(offset: 0))
        XCTAssertEqual(sealed, .sealed(offset: 0))
        XCTAssertNil(finished)
    }

    func testArgoDiffDocumentDefaultsVersionMetadataFlag() throws {
        let document = try JSONDecoder().decode(
            ArgoDiffDocument.self,
            from: Data(
                #"{"resources":[{"kind":"Deployment","namespace":"apps","name":"api","diff":"-old\n+new"}]}"#
                    .utf8
            )
        )

        XCTAssertEqual(document.resources.count, 1)
        XCTAssertEqual(document.resources[0].id, "Deployment/apps/api")
        XCTAssertFalse(document.resources[0].versionMetadataOnly)
    }

    func testProblemDetailBecomesLocalizedHTTPError() async throws {
        let session = makeSession()
        let client = SliprampAPIClient(
            baseURL: URL(string: "https://slipramp.example")!,
            token: "slipramp_key_invalid",
            session: session
        )

        StubURLProtocol.requestHandler = { request in
            let body = #"""
            {
              "type": "about:blank",
              "title": "Unauthorized",
              "status": 401,
              "detail": "The API key is invalid or expired."
            }
            """#.data(using: .utf8)!
            return (self.response(for: request, status: 401), body)
        }

        do {
            _ = try await client.getInbox()
            XCTFail("Expected the request to fail")
        } catch let error as SliprampAPIError {
            XCTAssertEqual(
                error,
                .http(
                    status: 401,
                    title: "Unauthorized",
                    detail: "The API key is invalid or expired."
                )
            )
            XCTAssertEqual(error.localizedDescription, "The API key is invalid or expired.")
        }
    }

    func testServerURLDefaultsToHTTPSAndRejectsCredentials() throws {
        XCTAssertEqual(
            try SliprampAPIClient.normalizeServerURL("slipramp.example/").absoluteString,
            "https://slipramp.example/"
        )
        XCTAssertThrowsError(
            try SliprampAPIClient.normalizeServerURL("https://user:secret@slipramp.example")
        )
    }

    func testTransportSecurityAllowsHTTPSAndOnlyPrivateHTTP() throws {
        XCTAssertEqual(
            try SliprampAPIClient.transportSecurity(
                for: URL(string: "https://slipramp.example.com")!
            ),
            .https
        )

        for value in [
            "http://localhost:8080",
            "http://slipramp.local",
            "http://slipramp",
            "http://10.20.30.40",
            "http://172.16.0.1",
            "http://192.168.1.4",
            "http://100.100.100.100",
            "http://[fd00::1]",
        ] {
            XCTAssertEqual(
                try SliprampAPIClient.transportSecurity(for: URL(string: value)!),
                .trustedPrivateHTTP,
                value
            )
        }

        for value in [
            "http://slipramp.example.com",
            "http://8.8.8.8",
            "http://[2001:4860:4860::8888]",
        ] {
            XCTAssertThrowsError(
                try SliprampAPIClient.transportSecurity(for: URL(string: value)!)
            ) { error in
                XCTAssertEqual(error as? SliprampAPIError, .publicHTTPNotAllowed, value)
            }
        }
    }

    func testCompatibilityCheckExplainsUntrustedCertificate() async throws {
        let session = makeSession()
        let client = SliprampAPIClient(
            baseURL: URL(string: "https://slipramp.example")!,
            session: session
        )
        StubURLProtocol.requestHandler = { _ in
            throw URLError(.serverCertificateUntrusted)
        }

        do {
            _ = try await client.getClientCompatibility()
            XCTFail("Expected the request to fail")
        } catch let error as SliprampAPIError {
            XCTAssertEqual(error, .connectionFailed(.certificate))
            XCTAssertEqual(
                error.localizedDescription,
                "The Server’s TLS certificate is not trusted for this address. Use a system-trusted certificate with the correct hostname."
            )
        }
    }

    private func makeSession() -> URLSession {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [StubURLProtocol.self]
        return URLSession(configuration: configuration)
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

    private func requestBody(_ request: URLRequest) throws -> Data {
        if let body = request.httpBody {
            return body
        }

        let stream = try XCTUnwrap(request.httpBodyStream)
        stream.open()
        defer { stream.close() }

        var body = Data()
        var buffer = [UInt8](repeating: 0, count: 4096)
        while stream.hasBytesAvailable {
            let count = stream.read(&buffer, maxLength: buffer.count)
            if count < 0 {
                throw try XCTUnwrap(stream.streamError)
            }
            if count == 0 {
                break
            }
            body.append(buffer, count: count)
        }
        return body
    }

    private func runBody(
        id: String,
        phase: String,
        outcome: String? = nil,
        reviewState: String,
        rerunOfRunID: String? = nil,
        rerunMode: String? = nil
    ) -> Data {
        let outcomeField = outcome.map { ",\"outcome\":\"\($0)\"" } ?? ""
        let rerunFields = rerunOfRunID.flatMap { parentID in
            rerunMode.map { mode in
                ",\"rerunOfRunId\":\"\(parentID)\",\"rerunMode\":\"\(mode)\""
            }
        } ?? ""
        return Data(
            """
            {
              "id": "\(id)",
              "phase": "\(phase)"\(outcomeField)\(rerunFields),
              "targetId": "929e7e9d-102a-43d0-9ca6-fc4d53dfbb14",
              "repositoryId": "ff281fe6-d60b-4312-840c-df2714f89717",
              "catalogRevisionId": "ced99dba-bb0b-4a8a-a9be-a5ef9875b945",
              "commit": { "sha": "0123456789abcdef0123456789abcdef01234567" },
              "target": "homelab",
              "action": "sync",
              "actionTitle": "Sync homelab",
              "parameters": {},
              "runtime": "terraform",
              "placement": "primary",
              "cluster": "colo",
              "source": { "type": "manual" },
              "inputs": {},
              "artifactConsumers": {},
              "review": { "state": "\(reviewState)" },
              "createdAt": "2026-10-30T12:34:56Z"
            }
            """.utf8
        )
    }

    private func targetListBody() -> Data {
        Data(
            """
            {
              "items": [
                {
                  "id": "929e7e9d-102a-43d0-9ca6-fc4d53dfbb14",
                  "repositoryId": "ff281fe6-d60b-4312-840c-df2714f89717",
                  "repositoryName": "infrastructure",
                  "catalogRevisionId": "ced99dba-bb0b-4a8a-a9be-a5ef9875b945",
                  "catalogCommit": { "sha": "0123456789abcdef0123456789abcdef01234567" },
                  "name": "homelab",
                  "title": "Homelab",
                  "actions": ["preview", "sync"],
                  "runtime": "terraform",
                  "placement": "primary",
                  "desiredCommit": { "sha": "89abcdef0123456789abcdef0123456789abcdef" },
                  "reconciliation": { "comparison": "matched" }
                }
              ],
              "total": 1,
              "limit": 100,
              "offset": 0
            }
            """.utf8
        )
    }

    private func promotionBody(id: String, status: String, decided: Bool = false) -> Data {
        let decision = decided
            ? """
            ,"decision": {
              "decision": "approved",
              "comment": "Diffs match the change request.",
              "decidedBy": {
                "principalId": "96b1acb7-2b65-4a0f-bae4-a5d9e2d069c0",
                "kind": "user",
                "displayName": "Ian Operator"
              },
              "decidedAt": "2026-10-30T13:00:00Z"
            },
            "decidedAt": "2026-10-30T13:00:00Z"
            """
            : ""

        return Data(
            """
            {
              "id": "\(id)",
              "source": "kargo",
              "integration": "kargo-primary",
              "externalPromotionId": "promote-production-42",
              "stageId": "281ba55d-fb9f-4f4e-862c-8c060c50b46c",
              "stageSnapshot": {
                "id": "281ba55d-fb9f-4f4e-862c-8c060c50b46c",
                "integration": "kargo-primary",
                "cluster": "colo",
                "project": "platform",
                "name": "production",
                "kubernetesUid": "stage-uid",
                "resourceVersion": "123",
                "application": "slipramp",
                "environment": "production",
                "repository": "homelab",
                "repositoryUrl": "https://git.example/homelab.git",
                "targetBranch": "main",
                "sourcePaths": ["deploy/slipramp"],
                "requestedFreight": [],
                "freight": { "items": [] },
                "lastObservedAt": "2026-10-30T12:30:00Z"
              },
              "repositoryId": "ff281fe6-d60b-4312-840c-df2714f89717",
              "repositorySnapshot": {
                "name": "homelab",
                "cloneUrl": "https://git.example/homelab.git",
                "branch": "main"
              },
              "baseCommit": {
                "sha": "0123456789abcdef0123456789abcdef01234567",
                "title": "Current production"
              },
              "candidateCommit": {
                "sha": "89abcdef0123456789abcdef0123456789abcdef",
                "title": "Upgrade Slipramp"
              },
              "candidateBranch": "slipramp/promotions/candidate-42",
              "targetBranch": "main",
              "policySnapshot": {
                "configuredMode": "review-required",
                "effectiveMode": "review-required"
              },
              "selectedFreight": {
                "origin": "git",
                "name": "freight-42",
                "artifacts": [
                  {
                    "kind": "image",
                    "repository": "ghcr.io/slipramp/slipramp",
                    "version": "v0.42.0"
                  }
                ]
              },
              "reviewRequired": true,
              "status": "\(status)",
              "dispatchAttemptCount": 0,
              "gitDiffArtifactId": "2a76876f-bf1b-4851-b401-16c68608962c",
              "argoDiffArtifactId": "28a6a716-6912-49b0-aabc-e41599de592b",
              "requestedBy": {
                "principalId": "96b1acb7-2b65-4a0f-bae4-a5d9e2d069c0",
                "kind": "user",
                "displayName": "Ian Operator"
              },
              "createdAt": "2026-10-30T12:34:56Z",
              "updatedAt": "2026-10-30T12:45:00Z"
              \(decision)
            }
            """.utf8
        )
    }
}

private final class StubURLProtocol: URLProtocol {
    static var requestHandler: ((URLRequest) throws -> (HTTPURLResponse, Data))?

    override class func canInit(with request: URLRequest) -> Bool {
        true
    }

    override class func canonicalRequest(for request: URLRequest) -> URLRequest {
        request
    }

    override func startLoading() {
        guard let handler = Self.requestHandler else {
            XCTFail("StubURLProtocol.requestHandler was not set")
            return
        }

        do {
            let (response, data) = try handler(request)
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: data)
            client?.urlProtocolDidFinishLoading(self)
        } catch {
            client?.urlProtocol(self, didFailWithError: error)
        }
    }

    override func stopLoading() {}
}
