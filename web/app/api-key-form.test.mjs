import assert from "node:assert/strict";
import test from "node:test";

import { apiKeyCreateBody } from "./api-key-form.ts";

test("parses the comma-separated scopes submitted by a Mantine checkbox group", () => {
  const formData = new FormData();
  formData.set("name", "deployment automation");
  formData.set("expiresAt", "2027-01-01T00:00:00Z");
  formData.set("scopes", "slipramp:read,slipramp:runs:write");

  assert.deepEqual(apiKeyCreateBody(formData), {
    expiresAt: "2027-01-01T00:00:00Z",
    name: "deployment automation",
    scopes: ["slipramp:read", "slipramp:runs:write"],
  });
});

test("accepts repeated scope fields and ignores unknown values", () => {
  const formData = new FormData();
  formData.set("name", "read-only automation");
  formData.append("scopes", "slipramp:read");
  formData.append("scopes", "unknown");

  assert.deepEqual(apiKeyCreateBody(formData), {
    expiresAt: undefined,
    name: "read-only automation",
    scopes: ["slipramp:read"],
  });
});

test("accepts the Kargo Promotion scopes", () => {
  const formData = new FormData();
  formData.set("name", "kargo production");
  formData.set("scopes", "slipramp:promotions:submit,slipramp:promotions:write");

  assert.deepEqual(apiKeyCreateBody(formData), {
    expiresAt: undefined,
    name: "kargo production",
    scopes: ["slipramp:promotions:submit", "slipramp:promotions:write"],
  });
});

test("accepts the Inbox acknowledgement scope", () => {
  const formData = new FormData();
  formData.set("name", "operations console");
  formData.set("scopes", "slipramp:read,slipramp:inbox:write");

  assert.deepEqual(apiKeyCreateBody(formData), {
    expiresAt: undefined,
    name: "operations console",
    scopes: ["slipramp:read", "slipramp:inbox:write"],
  });
});
