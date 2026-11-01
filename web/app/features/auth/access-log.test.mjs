import assert from "node:assert/strict";
import test from "node:test";

import { accessLogURL } from "./access-log.mjs";

test("redacts OIDC callback query parameters from access logs", () => {
  assert.equal(
    accessLogURL({ originalUrl: "/api/v1/auth/callback?code=secret&state=secret" }),
    "/api/v1/auth/callback",
  );
  assert.equal(accessLogURL({ originalUrl: "/runs?phase=running" }), "/runs?phase=running");
});
