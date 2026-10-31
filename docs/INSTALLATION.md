# Kubernetes Installation and Releases

Slipramp deploys one central Server and Web process, plus one Controller in
each execution Cluster. The Helm chart guides remain the authoritative
references for chart values and deployment-specific configuration.

## Deployment topology

[`deploy/charts/slipramp`](../deploy/charts/slipramp/README.md) installs one Server
and one web process. It requires external PostgreSQL and S3-compatible storage,
uses operator-managed Secret references by default, rolls the Server when its
Placement or Kargo integration ConfigMap changes, and gives the Server neither
RBAC nor a mounted service-account token. It can create an optional Ingress,
Gateway API HTTPRoute, or both; each sends API, Controller, execution, and
health paths to the Server and the remaining web path to the web process. The
Server also exposes `GET /metrics` on a dedicated port for trusted in-cluster
scraping; the chart deliberately leaves that port out of its public routes.

Install [`deploy/charts/slipramp-controller`](../deploy/charts/slipramp-controller/README.md)
once in each execution Cluster. It requires one stable Cluster name and one or
more exclusive execution namespaces, creates only namespaced Job/Pod RBAC, and
fixes the replica count at one. Git and execution Secrets stay in that Cluster.
Both charts use explicit non-`latest` image tags and expose resources, security
contexts, image pull Secrets, and focused scheduling settings.

## Installing published charts

Version tags publish matching application images and both charts to GHCR. OCI
installs pin the chart version; Helm infers the chart name from the final path
component and does not use `helm repo add` or an `index.yaml`.

```console
helm upgrade --install slipramp oci://ghcr.io/slipramp/slipramp-charts/slipramp --version 0.1.0 --namespace slipramp-system --create-namespace --values server-values.yaml
helm upgrade --install slipramp-controller oci://ghcr.io/slipramp/slipramp-charts/slipramp-controller --version 0.1.0 --namespace slipramp-system --values controller-values.yaml
```

## Installing from source

Source checkouts remain installable from `deploy/charts/slipramp` and
`deploy/charts/slipramp-controller`. GHCR creates new container packages as
private by default. After the first release, a maintainer must verify that the
six image packages and two chart packages are public for anonymous installs.
Private packages require `helm registry login ghcr.io` and matching Kubernetes
image pull credentials.

## Publishing a release

To publish a release, update both charts' `version` and `appVersion` fields and
every default image tag to the same version, then run the package check and
push the matching Git tag. The release workflow publishes the six images
first and only pushes the two OCI charts after every image succeeds.

```console
task charts:check
task charts:package CHART_VERSION=0.1.0
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```
