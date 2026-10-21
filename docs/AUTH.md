# Authentication and Identity Plan

This document describes the intended direction for product authentication,
authorization, and actor attribution in Slipramp. It is a planning aid for
incremental implementation, not a stable external contract.

Slipramp is primarily for homelabs and small teams. The design should provide a
sound security boundary and useful attribution without becoming a general
identity platform.

## Goals

- Require browser login through one configured OpenID Connect provider as the
  installation's root product-authentication path.
- Support API keys for people, scripts, and shared automation.
- Resolve every product credential to one durable Slipramp principal.
- Record who created and canceled a Run without confusing actor identity with
  the Run source.
- Apply a small installation-wide authorization model.
- Leave a clean path for a remote MCP server to reuse the same principals and
  permissions.
- Keep controller and execution authentication isolated from product-user
  authentication.

## Non-goals

The initial design does not include:

- organizations, tenants, workspaces, or nested teams;
- SAML, LDAP, SCIM, directory synchronization, or an authentication plugin
  system;
- a Slipramp-hosted OAuth authorization server;
- per-Repository, per-Target, or per-Action access-control lists;
- custom roles or a policy engine;
- delegated administration or enterprise governance flows;
- mandatory approvals or preview gates;
- high-availability session infrastructure beyond PostgreSQL.

These features should not be anticipated through unused abstractions. A real
later need can introduce a deliberate breaking change while Slipramp's
contracts remain unstable.

## Trust Boundaries

Product authentication remains separate from existing execution credentials:

- Product users and automation authenticate as a principal for `/api/v1` and,
  later, `/mcp`.
- Cluster Controllers retain cluster-scoped enrollment and bearer credentials
  for `/controller/v1`.
- Execution Jobs retain Run-scoped artifact credentials for `/execution/v1`.
- `/healthz` remains unauthenticated and contains no sensitive product data.

Controller and Run artifact credentials must not become product principals or
be accepted by product routes.

The Slipramp Server owns product authentication. The React Router web process
does not become a second identity provider or invent a trusted user-header
protocol. It forwards the browser's Slipramp session when making SSR API
requests.

## Core Model

### Principal

A principal is the durable product actor to which authorization and historical
attribution attach. It has a stable UUID, display name, kind, small global role,
and enabled or disabled state.

The initial kinds are:

- `user`: a person who normally signs in through OIDC;
- `service-account`: shared or unattended automation using an API key.

Service accounts prevent shared automation from impersonating a person or
breaking when a user is disabled.

### OIDC identity

An OIDC identity maps the authoritative pair `(issuer, subject)` to one user
principal. Email, preferred username, and display name are profile attributes,
not identity keys. Slipramp must not automatically link identities by email.

The first implementation only needs one configured provider. The data model can
still use `(issuer, subject)` as its correct durable key without adding a
multi-provider user experience.

### Credential

Web sessions and API keys authenticate a principal. They are not principals and
do not own authorization grants.

Authentication middleware should give handlers a result containing:

- principal ID, kind, display name, role, and disabled state;
- authentication method;
- credential ID and label when applicable;
- any scope restriction carried by the credential.

Handlers and domain services should consume this result instead of parsing
cookies, bearer tokens, or OIDC claims themselves.

## Persistence Sketch

The first persistence milestone implements these relationships:

```text
principals
  id
  kind                    user | service-account
  display_name
  role                    admin | operator | viewer
  disabled_at
  created_at
  updated_at

oidc_identities
  id
  principal_id            -> principals.id
  issuer
  subject
  profile fields          informational
  last_authenticated_at
  created_at
  UNIQUE (issuer, subject)

api_keys
  id
  principal_id            -> principals.id
  created_by_principal_id -> principals.id
  name
  token_prefix
  token_digest
  scopes
  created_at
  expires_at               optional
  last_used_at
  revoked_at

web_sessions
  id
  principal_id            -> principals.id
  token_digest
  created_at
  expires_at
  last_seen_at
```

Credential digests are stored as 32-byte SHA-256 values. API-key scopes use a
small validated set (`slipramp:read`, `slipramp:runs:write`, and
`slipramp:admin`) without introducing resource grants or a policy language.
Run creation and cancellation each store a principal foreign key plus an
immutable snapshot containing the principal ID, kind, and display name.

Principals and API keys referenced by history are disabled or revoked rather
than physically deleted. Expired sessions may be deleted.

## OIDC and Browser Sessions

The Server acts as an OIDC relying party using the authorization-code flow with
PKCE, state, and nonce validation. It validates the issuer, signature, audience,
time claims, and nonce before resolving `(issuer, subject)`.

After login, Slipramp issues an opaque local session cookie and stores only its
digest. Provider client secrets remain deployment configuration and are never
stored in PostgreSQL. The initial implementation should avoid retaining
provider access or refresh tokens unless a concrete provider requires them.

The Server owns login, callback, logout, and current-principal behavior. The
OpenAPI contract describes endpoints used by the web application. Cookie-based
state changes require CSRF protection; the exact mechanism can be chosen with
the web implementation.

Successful login to an issuer does not automatically imply enrollment in a
Slipramp installation. The implemented enrollment rule is an explicit
configuration map from OIDC subject to global role. Unknown subjects are
rejected, at least one mapped subject must be an administrator, and the
configured role is reapplied on every successful login. This establishes the
first administrator through OIDC without email linking or a claim-expression
language. Removing a subject prevents new logins; existing sessions remain
valid until their fixed expiry or revocation.

Local sessions have a configurable absolute lifetime of 12 hours by default
and no idle expiry. `last_seen_at` is refreshed for observability but does not
extend the session. Authorization transactions are short-lived, one-time,
in-memory records containing the PKCE verifier and nonce; restarting the Server
therefore requires an in-progress login to restart. The browser carries only a
matching HTTP-only, SameSite=Lax state cookie. Logout requires the custom
`X-Slipramp-CSRF: 1` header in addition to the session cookie.

## API Keys

API keys use `Authorization: Bearer`. A recognizable prefix distinguishes a
Slipramp key from an external OAuth access token without trial-validating it
against unrelated mechanisms.

The initial management surface should support:

- listing key metadata without exposing secrets;
- creating a key for the current user;
- creating a service account and its keys as an administrator;
- selecting coarse scopes;
- optional expiry;
- idempotent revocation.

Keys contain a high-entropy random secret, are displayed once, and are stored
only as a digest plus a non-secret identifier or prefix. Rotation is
create-new, update-client, revoke-old; no separate rotation protocol is needed.
The token encoding is `slipramp_key_` followed by an unpadded Base64URL encoding
of 32 random bytes. PostgreSQL stores the SHA-256 digest of the complete token
and a display prefix containing the marker plus the first 12 encoded secret
characters.

User API keys can be created only from that user's browser session, preventing
one API key from minting replacement user credentials. Administrators may use a
session or an administrator-scoped API key to create service accounts and their
keys. Revocation is allowed for the owning principal with any valid API-key
scope, or for a caller with both the administrator role and scope.

There is no bootstrap API key. The first configured administrator signs in
through OIDC, then creates service accounts and automation keys through the
normal authenticated management API. Automated tests follow the same boundary
using a disposable OIDC provider.

## Authorization

Authorization is installation-wide at first. The proposed roles are:

- `viewer`: read product state, Run history, logs, and permitted artifacts;
- `operator`: viewer capabilities plus creating and canceling Runs;
- `admin`: operator capabilities plus repository and identity administration.

The exact operation matrix should be written with the OpenAPI changes. Artifact
sensitivity deserves an explicit decision rather than being inferred from a
generic read permission.

The initial permission matrix is:

| Permission | Operations | Minimum role | API-key scope |
| --- | --- | --- | --- |
| `read` | List repositories, targets, clusters, Runs, logs, and artifact metadata; get catalogs and Runs; download non-sensitive artifacts | `viewer` | `slipramp:read` |
| `runs:write` | Create and cancel Runs | `operator` | `slipramp:runs:write` |
| `admin` | Create and remove repositories; manage service accounts and their API keys | `admin` | `slipramp:admin` |
| `api-keys:revoke` | Revoke an API key owned by the current principal | `viewer` | Any valid scope |

The `slipramp:admin` scope grants every permission, subject to the principal's
role. Other scopes do not imply one another, so an operator API key that must
both inspect state and create Runs needs both `slipramp:read` and
`slipramp:runs:write`.

Viewers may list sensitive artifact metadata but may not download sensitive
artifact contents. Sensitive downloads require an `operator` or `admin`
principal and, for API keys, `slipramp:read`. This is a resource-sensitive
check in addition to the download operation's ordinary `read` permission.

Credentials may carry coarse scopes such as `slipramp:read`,
`slipramp:runs:write`, and `slipramp:admin`. A credential scope can restrict the
principal's role but cannot expand it. Custom roles, resource grants, deny
rules, and a policy language remain out of scope.

All product endpoints except the OIDC login and callback boundary require
authentication. Current-principal and logout authenticate their session inside
the operation. OpenAPI declares product session and bearer security schemes
while retaining the existing controller and Run artifact schemes.

## Run Source and Actor Attribution

Run source records which boundary submitted a Run. Actor identity records the
principal responsible for it. The client should no longer choose the source:

- a web action uses `manual`;
- the versioned HTTP API uses `api`;
- a future MCP adapter uses `mcp`;
- future trigger adapters use their corresponding trigger source.

The boundary determines source even when multiple boundaries accept the same
credential type. Authentication method remains audit data, not a Run source.

Runs retain a principal reference and a small immutable actor snapshot so
historical display remains meaningful after a rename or disable. At minimum,
persist attribution for Run creation and cancellation. Scope authenticated Run
idempotency keys to the submitting principal rather than globally to a source
type.

Existing Runs may have a null actor during migration. Newly authenticated Runs
must have one.

A general audit-event table may become useful for repository and identity
administration, but it is not required for the first authenticated release. Add
one only when there are enough administrative operations to define a coherent
event model.

## Web Application Integration

React Router loaders and actions must forward the incoming Slipramp session to
the Go API. The handwritten log-stream and artifact-download proxies do the
same without forwarding unrelated browser headers.

The initial UI only needs login redirects, current-principal display, logout,
and basic API-key management. It may hide unavailable actions for usability,
but the Server remains authoritative. The web process must not use a
deployment-wide privileged key on behalf of browser users.

## MCP Direction

MCP is a future adapter over the same principals and application services. It
does not introduce an MCP-specific user table or bypass normal authorization.

A remote MCP server should follow the then-current MCP authorization
specification and act as an OAuth resource server. It advertises protected
resource metadata, validates tokens intended for its resource, and enforces
scopes using an external authorization server. Slipramp should not become an
OAuth authorization server.

OIDC login support does not guarantee that the provider can issue a suitable
MCP access token. The MCP milestone must verify issuer, audience or resource,
scope, and client-registration behavior against the current specification:
<https://modelcontextprotocol.io/specification/latest/basic/authorization>.

Clients supporting a static bearer header may use a Slipramp API key as a
convenience. That does not replace standard OAuth discovery for general remote
MCP clients.

## Delivery Milestones

Each milestone is intended to fit in an independent implementation session.

### 1. Principal and attribution persistence — complete

- Add focused migrations, queries, types, and services for principals and
  credential metadata.
- Add Run creator and cancellation attribution with immutable snapshots.
- Scope authenticated Run idempotency to the principal.
- Add persistence and migration integration tests.

This milestone did not expose credentials or protect routes. The completed
authentication work now requires actors for new product Runs and scopes
idempotency keys to the creating principal.

### 2. Authentication and authorization boundaries — complete

- Add a product authentication result to request context.
- Add operation-level authorization using the small global role model.
- Keep controller and execution authentication independent.
- Define product security schemes and authentication errors in OpenAPI.
- Test missing, invalid, disabled, and insufficiently authorized callers.

The implemented boundary defines validated product authentication results,
the role-and-scope permission intersection, operation-level middleware, and
the product session and bearer security schemes with standard `401` and `403`
responses. The live Server now registers the middleware for every product
operation. Controller and Run artifact authentication continue through their
independent credential paths.

### 3. OIDC login and local sessions — complete

- Select and document enrollment and first-administrator behavior.
- Add provider discovery, login, callback, and logout.
- Create or update the identity mapping after validated login.
- Issue, validate, expire, and revoke local sessions.
- Expose the current principal through the generated contract.
- Test claim validation, mapping, disabled principals, expiry, and CSRF.

The implemented Server surface provides login, callback, current-principal,
and logout operations through the generated contract. It discovers one OIDC
provider, uses authorization code flow with PKCE, validates state and nonce,
creates or refreshes `(issuer, subject)` identities, and stores only local
session digests. OIDC configuration and at least one administrator mapping are
required at startup.

### 4. Web authentication — complete

- Forward sessions through SSR calls and handwritten proxies.
- Add login redirects, principal display, and logout.
- Remove caller-supplied Run source from the UI and API contract.
- Derive manual source and actor attribution at the Server boundary.
- Apply consistent authorization to logs and artifact downloads.

The implemented web process forwards only the opaque Slipramp session cookie
to generated SSR client calls and the handwritten log and artifact proxies. It
also exposes same-origin login and callback pass-through routes, displays the
current principal and role, and performs CSRF-protected logout without becoming
an identity provider itself.

The Server authenticates and authorizes product operations from the local
session. Manual Run creation no longer accepts a source from the request
contract: the web boundary assigns `manual` and attaches the authenticated
principal to creation and cancellation. API-source derivation is applied for
API-key callers.

The same session reaches live-log and artifact-download requests. In
authenticated mode, sensitive artifact metadata remains readable to viewers,
while downloading sensitive contents requires an operator or administrator.

### 5. API keys and service accounts — complete

- Implement generation, digest lookup, authentication, last-use recording,
  expiry, and revocation.
- Add the minimal management API and web UI.
- Support service accounts without team or ownership hierarchies.
- Derive API source and actor attribution at the Server boundary.
- Use API keys in end-to-end coverage instead of an auth bypass.

The completed API-key surface generates prefixed 256-bit secrets, stores only
their digest and display prefix, authenticates bearer credentials, records last
use, rejects expired, revoked, or disabled credentials, and supports idempotent
owner-or-administrator revocation. API-key-authenticated Run creation derives
the `api` source at the Server boundary.

The generated management API and Access screen list key metadata, create user
keys from browser sessions, display new secrets once, and let administrators
create service accounts and their keys. The disposable-cluster acceptance flow
signs in through its Dex provider as the configured administrator, creates an
API key through the same management API, and uses that bearer credential for
the remaining product requests.

### 6. Complete product-route enforcement — complete

- Require authentication on every `/api/v1` operation.
- Apply the reviewed role and scope matrix consistently.
- Remove temporary rollout or test bypasses.
- Update deployment, configuration, backup, and recovery documentation.
- Run generated-output, unit, integration, lint, and build checks.

The Server now requires complete OIDC configuration, discovers the provider at
startup, and rejects missing or invalid credentials for every product
operation. OpenAPI no longer advertises an anonymous alternative. The reviewed
role-and-scope matrix is enforced by one fail-closed middleware, with the
additional operator requirement for sensitive artifact contents. Identity
recovery uses the explicit OIDC subject-role mapping; no bootstrap credential
or authentication bypass remains. The acceptance environment deploys Dex and
exercises OIDC login before creating its automation key.

### 7. MCP adapter, later

- Recheck the current MCP transport and authorization specifications.
- Confirm the authorization server can issue resource-bound tokens.
- Map validated token identity to the existing principal model.
- Reuse application authorization and Run creation services.
- Add MCP-specific scopes only if the tool surface demonstrates a need.

## Deferred Decisions

Resolve these in the milestone that first needs them:

- when administrative operations justify a general audit-event table.

Choose the smallest behavior that meets the actual deployment need. Do not
resolve these questions by adding a general policy framework.
