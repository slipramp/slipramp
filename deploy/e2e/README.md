# Homelab end-to-end test

This setup keeps the Slipramp Server, PostgreSQL, RustFS, and web process on the
development laptop. The homelab controller and execution Jobs reach the Server
at the laptop's Tailscale IP. Tailscale encrypts the transport, so the first test
uses plaintext HTTP inside that trusted tailnet instead of adding a second TLS
layer.

The first Run should be the `homelab-node-exporter` `preview` action. It is a
pyinfra dry run, but still exercises repository refresh, immutable catalog and
Run resolution, controller enrollment and polling, Kubernetes Job creation,
the exact-commit checkout, Secret injection, the harness, durable log streaming,
and terminal observation.

## 1. Publish the execution images

The root [`Dockerfile`](../../Dockerfile) contains separate `controller`,
`harness`, and `checkout` targets. The image workflow publishes Linux AMD64
images to GHCR after these changes land on `main`.

Resolve all three published tags to immutable digests. The controller deployment
script rejects mutable tags:

```console
export SLIPRAMP_CONTROLLER_IMAGE='ghcr.io/slipramp/slipramp-controller@sha256:<digest>'
export SLIPRAMP_HARNESS_IMAGE='ghcr.io/slipramp/slipramp-harness@sha256:<digest>'
export SLIPRAMP_CHECKOUT_IMAGE='ghcr.io/slipramp/slipramp-checkout@sha256:<digest>'
```

## 2. Add the catalog to `kcloud-ops`

Copy the two files below into the real repository and commit them to `main`:

```console
mkdir -p /Users/ianunruh/Workspace/kcloud-ops/.slipramp
cp examples/kcloud-ops/.slipramp/runtimes.yaml /Users/ianunruh/Workspace/kcloud-ops/.slipramp/
cp examples/kcloud-ops/.slipramp/targets.yaml /Users/ianunruh/Workspace/kcloud-ops/.slipramp/
```

The checked-in `deploys/slipramp/node-exporter.sh` already supplies the action
boundary needed by the first Run. Copy the Terraform `slipramp.sh` launcher from
the example before testing the later plan-artifact promotion flow.

## 3. Start the laptop services

Create independent controller enrollment, controller bearer, and artifact-token
secrets. Preserve these environment variables in both shells used below without
writing them into Git:

```console
export SLIPRAMP_CONTROLLER_ENROLLMENT_TOKEN="$(openssl rand -hex 32)"
export SLIPRAMP_CONTROLLER_TOKEN="$(openssl rand -hex 32)"
export SLIPRAMP_ARTIFACT_TOKEN_SECRET="$(openssl rand -hex 32)"
export SLIPRAMP_SERVER_ADDRESS='<tailscale-ip>:8080'
export SLIPRAMP_SERVER_URL="http://${SLIPRAMP_SERVER_ADDRESS}"
```

Start the durable dependencies and then the Server:

```console
task local:up
./scripts/e2e/run-local-server.sh
```

The Server uses [`examples/kcloud-ops/placements.yaml`](../../examples/kcloud-ops/placements.yaml)
by default. It listens only on the configured Tailscale IP. Its PostgreSQL and
RustFS clients continue to use the laptop-local development endpoints.

In another shell, start the web application:

```console
SLIPRAMP_API_URL="$SLIPRAMP_SERVER_URL" task web:dev
```

## 4. Deploy the homelab controller

The controller and execution images are private. Provide a GitHub username and
a token with `read:packages`, or point at a portable Docker config containing
inline `ghcr.io` credentials. Docker configs that only name a desktop credential
store are rejected because Kubernetes cannot use that store. Also provide the
Git checkout identity, pinned GitHub host keys, and the pyinfra deployment key:

```console
export SLIPRAMP_GHCR_USERNAME='<github-username>'
export SLIPRAMP_GHCR_TOKEN='<read-packages-token>'
export SLIPRAMP_GIT_SSH_KEY='<absolute-path-to-github-read-key>'
export SLIPRAMP_GIT_KNOWN_HOSTS='<absolute-path-to-pinned-known-hosts>'
export SLIPRAMP_HOST_SSH_KEY='<absolute-path-to-deploybot-ops-key>'
./scripts/e2e/deploy-homelab-controller.sh
```

To use a Docker config instead, set `SLIPRAMP_GHCR_DOCKER_CONFIG` and leave the
username and token unset.

The script applies the zero-replica manifests from
[`homelab`](./homelab), creates or updates the required Secrets and ConfigMap,
sets the immutable controller image, and only then scales the controller to one
replica. Secret values are sent directly to Kubernetes and are never written to
the repository.

The kcloud-ops runtime executes as a non-root user. Its SSH credential uses mode
`0444` because Kubernetes Secret files are root-owned; the credential volume is
still read-only and is mounted only into the action container.

The controller has cluster-wide list access to its managed Jobs because restart
recovery scans every placement namespace. It can otherwise only create, get,
list, and delete Jobs; read Pods and their logs; and update the cancellation
annotation on managed Pods.

The `infrastructure-admin` ServiceAccount used by the example placement is
bound to the built-in `cluster-admin` role. Runs using that placement can make
unrestricted changes to the homelab cluster, including draining nodes during a
Kubernetes upgrade. Use a narrower ServiceAccount for untrusted repositories or
actions.

## 5. Register and run `kcloud-ops`

Open the repository form and use:

- Name: `kcloud-ops`
- Clone URL: `git@github.com:ianunruh/kcloud-ops.git`
- Branch: `main`
- Refresh credential: empty when the local Server can use the current SSH agent
- Checkout credential: `kcloud-ops`

Wait for a successful refresh, open `homelab-node-exporter`, and run `preview`.
The expected result is a successful Run with a non-empty retained log artifact.
Do not run `canary`, `sync`, or either RustFS mutation until the preview path has
completed and its output has been reviewed.

Useful observations during the test:

```console
kubectl --context homelab -n slipramp-system logs deployment/slipramp-controller -f
kubectl --context homelab -n slipramp-runs get jobs,pods
```
