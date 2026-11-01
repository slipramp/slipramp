# Identity and authorization

This document defines product Principals, authentication boundaries, sessions,
API keys, roles, scopes, and HTTP protections.

## Authentication boundaries

The Slipramp Server owns product authentication and authorization.

- `/api/v1` accepts product sessions or Slipramp API keys, except for public OIDC
  handoff and non-sensitive client compatibility.
- `/mcp` accepts scoped Slipramp API keys or OAuth access tokens from one
  configured external authorization server.
- `/controller/v1` and `/execution/v1` use independent Cluster and Run-scoped
  credentials that never become product Principals.
- Health, readiness, and client compatibility expose only non-sensitive public
  information.

Domain services consume one authenticated authorization result. They do not
parse cookies or token claims independently, and clients never become the
authorization authority by hiding UI.

## Principals

A Principal is a user or service account with a stable UUID, display name,
global role, and disabled state. OIDC identities use `(issuer, subject)`; email
and profile names never link identities. Credentials authenticate a Principal
and may narrow its permissions, but they do not own grants.

Disabling a Principal immediately prevents all of its sessions and API keys from
authenticating. Enabling it restores credentials that remain unexpired and
unrevoked. Separate operations permanently revoke all API keys or remove all
local sessions. These emergency controls preserve credential metadata and do
not imply a general administrative audit log.

## OIDC and sessions

One configured OIDC provider is required. Browser login uses authorization code
flow with PKCE, state, nonce, issuer, signature, audience, and time validation.
Explicit `(issuer, subject)` configuration enrolls users and assigns roles;
unknown subjects are rejected. Provider access and refresh tokens are not
retained.

The Server issues opaque local sessions and stores only their SHA-256 digests.
Sessions have a fixed absolute lifetime without idle extension. Browser sessions
use an HTTP-only cookie. Native login uses the same provider flow plus an
app-generated PKCE challenge and short-lived one-time exchange code before the
same opaque session is stored in Keychain.

Login transactions are bounded and in memory; an interrupted login restarts
after Server restart. Anonymous starts are rate-limited by resolved client and
installation-wide capacity. Client IP resolution trusts forwarded addresses
only when an exact proxy-hop configuration is enabled.

Unsafe cookie-authenticated product requests require CSRF proof. The Web accepts
unsafe browser requests only from the same host before forwarding that proof.
Bearer requests do not use the browser CSRF header.

## API keys and permissions

API keys contain 32 random bytes, are displayed once, and are stored only as a
SHA-256 digest with display prefix, scopes, expiry, last use, and revocation.
Users create their own keys through a local session. Administrators create
service accounts and their keys through authenticated product operations. There
is no bootstrap API key or anonymous product bypass.

| Operation | Minimum role | API-key scope |
| --- | --- | --- |
| Read product state, history, logs, and non-sensitive Artifact contents | viewer | `slipramp:read` |
| Create, rerun, or cancel Runs | operator | `slipramp:runs:write` |
| Decide Run reviews | operator | `slipramp:runs:review` |
| Acknowledge Inbox problems | operator | `slipramp:inbox:write` |
| Submit or poll Kargo Promotions | operator service account | `slipramp:promotions:submit` |
| Create or decide Promotions and change Stage policy | operator | `slipramp:promotions:write` |
| Administer Repositories, Principals, credentials, and sessions | admin | `slipramp:admin` |

Scopes intersect with the Principal's role and do not imply one another.
`slipramp:admin` grants all scopes only when the Principal role also authorizes
the operation. Sensitive Artifact contents require operator or administrator
role in addition to read scope.

Historical Principals and keys are disabled or revoked rather than deleted.
API-key rotation is create, update clients, then revoke.

## MCP OAuth

Slipramp is an OAuth resource server, not an authorization server. When MCP
OAuth is enabled, the Server publishes protected-resource metadata, discovers
one external issuer, validates signature, issuer, exact resource audience, time,
subject, and scopes, and maps the subject through the existing OIDC enrollment
map. The authorization server does not become a second user store.

MCP tools invoke the same domain services and role checks as product requests.
Read and Run-write scopes remain distinct, and sensitive Artifact access retains
the product role requirement. See [MCP.md](../MCP.md) for client setup and tool
contracts.

## HTTP protections

Product, MCP, Controller, and execution responses are `private, no-store`.
Authentication happens before protected request bodies are decoded. Public and
authenticated JSON bodies have bounded sizes and read windows. Artifact uploads
require a declared content length and valid Run credential before the stream is
admitted.

Structured request logs omit query strings, credentials, parameter values, and
Artifact contents. OIDC callback codes and state therefore never enter access
logs. Secret values and secret file paths are excluded from product responses.
