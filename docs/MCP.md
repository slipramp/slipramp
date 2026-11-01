# Model Context Protocol

Slipramp exposes a remote Model Context Protocol endpoint at `/mcp`. It uses
stateless Streamable HTTP and a scoped Slipramp API key as a Bearer credential.
The adapter reuses the same Principals, authorization rules, catalog, Run, and
Artifact services as the product API.

## Tools and permissions

| Tool                    | Behavior                                       | Required permission |
| ----------------------- | ---------------------------------------------- | ------------------- |
| `list_targets`          | List active Targets and available Actions      | Read                |
| `explain_target_action` | Resolve an Action without executing it         | Read                |
| `create_run`            | Create an immutable Run with source `mcp`      | Run write           |
| `get_run`               | Read the persisted state and outcome of a Run  | Read                |
| `read_run_log`          | Read a bounded live or retained log page        | Read                |
| `list_run_artifacts`    | List Artifact metadata and report summaries     | Read                |
| `read_artifact`         | Read a bounded retained Artifact content page   | Read                |
| `cancel_run`            | Request cancellation of a queued or active Run | Run write           |

Permissions intersect the Principal's role and API-key scopes. A fully capable
key therefore needs an `operator` or `admin` Principal and both
`slipramp:read` and `slipramp:runs:write`. A read-only connection needs a
`viewer`, `operator`, or `admin` Principal and `slipramp:read`.

`create_run` requires an `idempotencyKey` of at most 128 characters. Generate a
new key for each logical Run and reuse it only when retrying the identical
request. The tool does not currently accept Artifact inputs, so Actions that
require them must be started through another product interface.

`read_run_log` and `read_artifact` return at most 64 KiB of source bytes per
call. Their `nextOffsetBytes` values can be supplied as the next call's
`offsetBytes` until `hasMore` is false. Text, JSON, and XML use `utf-8` output;
other content uses `base64`. Reading an open log at its current durable end
returns an empty page so clients can poll without holding a streaming MCP call.

Artifact metadata and non-sensitive contents require ordinary read permission.
Sensitive contents additionally require an `operator` or `admin` Principal;
the API key still uses the ordinary `slipramp:read` scope, matching the product
API authorization contract. Expired or deleted contents remain visible as
metadata but cannot be read.

## Connect Codex

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

The endpoint currently supports API-key Bearer authentication only. It does not
publish OAuth protected-resource metadata or start an interactive OAuth flow.
