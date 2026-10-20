import assert from "node:assert/strict";
import test from "node:test";

import { proxyArtifactDownload } from "./api/artifact-download.server.ts";

test("proxies immutable artifact bytes and download metadata", async () => {
  const artifactId = "018f90ff-9fd8-7b5f-b7e1-f32ff54c1202";
  let upstreamRequest;
  const requestUpstream = async (url, init) => {
    upstreamRequest = { init, url };

    return new Response("saved plan", {
      headers: {
        "content-disposition": 'attachment; filename="plan"',
        "content-length": "10",
        "content-type": "application/octet-stream",
        digest: "sha-256=abc123",
        "x-internal-header": "not forwarded",
      },
    });
  };

  const response = await proxyArtifactDownload(
    artifactId,
    new Request(`http://web.test/api/v1/artifacts/${artifactId}/download`, {
      headers: {
        cookie: "theme=dark; slipramp_session=session-token; analytics=disabled",
      },
    }),
    "http://server.test:8080/base/path",
    requestUpstream,
  );

  assert.equal(
    upstreamRequest.url.href,
    `http://server.test:8080/api/v1/artifacts/${artifactId}/download`,
  );
  assert.equal(upstreamRequest.init.headers.accept, "application/octet-stream");
  assert.equal(upstreamRequest.init.headers.cookie, "slipramp_session=session-token");
  assert.equal(response.status, 200);
  assert.equal(response.headers.get("content-disposition"), 'attachment; filename="plan"');
  assert.equal(response.headers.get("content-length"), "10");
  assert.equal(response.headers.get("digest"), "sha-256=abc123");
  assert.equal(response.headers.get("x-internal-header"), null);
  assert.equal(await response.text(), "saved plan");
});

test("preserves upstream artifact errors", async () => {
  const response = await proxyArtifactDownload(
    "018f90ff-9fd8-7b5f-b7e1-f32ff54c1202",
    new Request("http://web.test/api/v1/artifacts/missing/download"),
    "http://server.test:8080",
    async () =>
      Response.json(
        { detail: "The artifact does not exist.", status: 404, title: "Not Found" },
        { status: 404 },
      ),
  );

  assert.equal(response.status, 404);
  assert.equal(response.headers.get("content-type"), "application/json");
  assert.deepEqual(await response.json(), {
    detail: "The artifact does not exist.",
    status: 404,
    title: "Not Found",
  });
});

test("returns a gateway error when the Server is unavailable", async () => {
  const response = await proxyArtifactDownload(
    "018f90ff-9fd8-7b5f-b7e1-f32ff54c1202",
    new Request("http://web.test/api/v1/artifacts/unavailable/download"),
    "http://server.test:8080",
    async () => {
      throw new Error("connection refused");
    },
  );

  assert.equal(response.status, 502);
  assert.deepEqual(await response.json(), {
    detail: "The Slipramp Server could not be reached.",
    status: 502,
    title: "Artifact download unavailable",
    type: "about:blank",
  });
});
