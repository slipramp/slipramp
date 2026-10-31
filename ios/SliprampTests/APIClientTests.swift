import Foundation
import XCTest
@testable import SliprampCore

final class APIClientTests: XCTestCase {
    override func tearDown() {
        StubURLProtocol.requestHandler = nil
        super.tearDown()
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

        StubURLProtocol.requestHandler = { request in
            XCTAssertEqual(request.httpMethod, "GET")
            XCTAssertEqual(request.url?.path, "/api/v1/artifacts/\(artifactID)/download")
            XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer slipramp_key_secret")
            let response = HTTPURLResponse(
                url: request.url!,
                statusCode: 200,
                httpVersion: "HTTP/1.1",
                headerFields: ["Content-Type": "application/octet-stream"]
            )!
            return (response, Data("@@ -1 +1 @@\n-old\n+new\n".utf8))
        }

        let contents = try await client.downloadArtifactText(id: artifactID)
        XCTAssertEqual(contents, "@@ -1 +1 @@\n-old\n+new\n")
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
        reviewState: String
    ) -> Data {
        let outcomeField = outcome.map { ",\"outcome\":\"\($0)\"" } ?? ""
        return Data(
            """
            {
              "id": "\(id)",
              "phase": "\(phase)"\(outcomeField),
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
