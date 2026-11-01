# Model Context Protocol

Slipramp exposes a remote Model Context Protocol endpoint at `/mcp`. It uses
stateless Streamable HTTP and accepts either a scoped Slipramp API key or a JWT
OAuth access token as a Bearer credential. The adapter reuses the same
Principals, authorization rules, catalog, Run, and Artifact services as the
product API.

OAuth is optional. When configured, the Server publishes RFC 9728 metadata at
`/.well-known/oauth-protected-resource/mcp`, advertises `slipramp:read` and
`slipramp:runs:write`, and directs clients to the configured external
authorization server. It validates the token signature, issuer, exact `/mcp`
resource audience, lifetime, subject, and scopes. The authorization-server
issuer must exactly match `SLIPRAMP_OIDC_ISSUER`, and the subject must be enrolled
in `SLIPRAMP_OIDC_SUBJECT_ROLES`; it resolves to the same product Principal as
browser OIDC login.

## Tools and permissions

| Tool                    | Behavior                                       | Required permission |
| ----------------------- | ---------------------------------------------- | ------------------- |
| `list_targets`          | List active Targets and available Actions      | Read                |
| `explain_target_action` | Resolve an Action without executing it         | Read                |
| `create_run`            | Create an immutable Run with source `mcp`      | Run write           |
| `list_runs`             | List and filter recent Runs                    | Read                |
| `get_run`               | Read the persisted state and outcome of a Run  | Read                |
| `read_run_log`          | Read a bounded live or retained log page        | Read                |
| `list_run_artifacts`    | List Artifact metadata and report summaries     | Read                |
| `read_artifact`         | Read a bounded retained Artifact content page   | Read                |
| `cancel_run`            | Request cancellation of a queued or active Run | Run write           |

Permissions intersect the Principal's role and credential scopes. A fully
capable API key or OAuth token therefore needs an `operator` or `admin`
Principal and both
`slipramp:read` and `slipramp:runs:write`. A read-only connection needs a
`viewer`, `operator`, or `admin` Principal and `slipramp:read`.

`create_run` requires an `idempotencyKey` of at most 128 characters. Generate a
new key for each logical Run and reuse it only when retrying the identical
request. For an Action with Artifact inputs, pass `inputs` as an object mapping
every declared input name to a retained Artifact UUID. Use
`list_run_artifacts` on a producing Run to resolve those UUIDs. The ordinary Run
service verifies that each Artifact is sealed, retained, from the same Target,
produced by the declared Action, and compatible with the exact catalog
revision. The resulting Run is pinned to that producing revision and commit.

`create_run` and `get_run` return the frozen input metadata and provenance under
`inputs`; they never return Artifact contents there. Use `read_artifact` when
the contents are needed and permitted.

`list_runs` returns Runs in descending creation order. It supports the same
repository, commit, consumed Artifact, Target, cluster, phase, and review-state
filters as the product API, with `limit` and `offset` pagination. A commit
filter requires `repositoryId` so identical Git object IDs from different
Repositories never merge their histories.

`read_run_log` and `read_artifact` return at most 64 KiB of source bytes per
call. Their `nextOffsetBytes` values can be supplied as the next call's
`offsetBytes` until `hasMore` is false. Text, JSON, and XML use `utf-8` output;
other content uses `base64`. Reading an open log at its current durable end
returns an empty page so clients can poll without holding a streaming MCP call.
If a finished Run never produced a log, such as one canceled before assignment,
`read_run_log` returns `available: false` with an empty completed page and omits
the Artifact-specific fields.

Artifact metadata and non-sensitive contents require ordinary read permission.
Sensitive contents additionally require an `operator` or `admin` Principal;
the credential still uses the ordinary `slipramp:read` scope, matching the
product API authorization contract. Expired or deleted contents remain visible
as metadata but cannot be read.

## Operational telemetry

Every decoded tool invocation emits one structured `MCP tool call` log after
completion. The event records the fixed tool name, `succeeded`, `rejected`, or
`failed` result, duration, Principal and credential identifiers, authentication
method, MCP client name and version, protocol version, and applicable Target,
Run, Action, or Artifact identifiers. A rejected result is a caller-visible
authorization, validation, conflict, or not-found outcome; a failed result is
an internal service failure.

Tool arguments, parameter values, idempotency keys, Bearer credentials, log
contents, and Artifact contents are never included. The underlying SDK's
stateless session lifecycle messages are suppressed below warning level because
each HTTP exchange intentionally creates a short-lived session without a
session ID.

The Server also exports `slipramp_mcp_tool_calls_total` and
`slipramp_mcp_tool_duration_seconds` with only fixed `tool` and `result` labels.
See [METRICS.md](./METRICS.md) for the complete metric contract.

## Connect Codex with OAuth

Add the remote server without a fixed Bearer token:

```toml
[mcp_servers.slipramp]
url = "https://slipramp.example.com/mcp"
auth = "oauth"
scopes = ["slipramp:read", "slipramp:runs:write"]
```

Then start the authorization-code flow:

```console
codex mcp login slipramp
```

Codex follows the protected-resource metadata, uses dynamic client registration
when the authorization server advertises it, and performs authorization code
with S256 PKCE. The CLI and IDE extension share the MCP configuration; the IDE
also presents an **Authenticate** action. See the
[official Codex MCP configuration](https://learn.chatgpt.com/docs/extend/mcp).
Use HTTPS unless the connection already travels over a trusted encrypted network
such as a tailnet.

## Connect Codex with an API key

Create a user or service-account API key with the required scopes, keep its
one-time plaintext value outside the repository, and expose it to the local
Codex process:

```console
export SLIPRAMP_MCP_API_KEY='slipramp_key_...'
```

Add the server to `~/.codex/config.toml`, or to `.codex/config.toml` in a trusted
project:

```toml
[mcp_servers.slipramp]
url = "https://slipramp.example.com/mcp"
bearer_token_env_var = "SLIPRAMP_MCP_API_KEY"
```

Restart the Codex client, then verify the connection with `codex mcp list` or
`/mcp`. The Codex desktop app, CLI, and IDE extension share this configuration.
Use HTTPS unless the connection already travels over a trusted encrypted
network such as a tailnet.

OAuth and API-key authentication can remain enabled together. Tokens that do
not start with the Slipramp API-key prefix are treated as OAuth access tokens
only when OAuth configuration is enabled.
