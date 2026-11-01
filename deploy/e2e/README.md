# End-to-end tests

The supported end-to-end acceptance suites exercise Run execution and Kargo
Promotions in disposable kind clusters. They install the Server, web, and
Controller from their supported Helm charts while keeping test-only
dependencies as ordinary manifests. See the
[kind acceptance test](./kind/README.md) for its coverage, prerequisites, and
diagnostic options.

Run it from the repository root:

```console
task acceptance:kind
task acceptance:kargo
```
