package catalog_test

import (
	"testing"

	"github.com/slipramp/slipramp/internal/catalog"
)

func TestValidateRuntimeCredentials(t *testing.T) {
	t.Parallel()

	tests := []validationTest{
		{
			name: "complete injection",
			configure: func(input *catalog.Catalog) {
				input.Runtimes[0].Spec.WorkingDirectory = "/workspace"
				input.Runtimes[0].Spec.Environment = map[string]string{
					"TF_IN_AUTOMATION": "true",
				}
				input.Runtimes[0].Spec.Credentials = map[string]catalog.RuntimeCredential{
					"backend": {
						Inject: catalog.CredentialInjection{
							Environment: map[string]catalog.CredentialEnvironmentVariable{
								"AWS_ACCESS_KEY_ID": {Key: "accessKeyId"},
							},
							Files: []catalog.CredentialFile{{
								Key:  "config",
								Path: "/var/run/backend/config",
								Mode: 0o400,
							}},
						},
					},
					"plugins": {
						Inject: catalog.CredentialInjection{
							EnvironmentFrom: true,
							Directory: &catalog.CredentialDirectory{
								Path: "/var/run/plugins",
								Mode: 0o440,
							},
						},
					},
				}
				input.Targets[0].Spec.Credentials = map[string]string{
					"backend": "terraform-backend",
					"plugins": "terraform-plugins",
				}
			},
		},
		{
			name: "empty injection",
			configure: func(input *catalog.Catalog) {
				addRuntimeCredential(input, "backend", catalog.CredentialInjection{})
			},
			wantError: "must define at least one injection",
		},
		{
			name: "invalid environment name",
			configure: func(input *catalog.Catalog) {
				input.Runtimes[0].Spec.Environment = map[string]string{"INVALID-NAME": "value"}
			},
			wantError: "runtime environment name",
		},
		{
			name: "reserved environment name",
			configure: func(input *catalog.Catalog) {
				input.Runtimes[0].Spec.Environment = map[string]string{
					"SLIPRAMP_RUN_ID": "different-run",
				}
			},
			wantError: "reserved SLIPRAMP_ namespace",
		},
		{
			name: "environment collision",
			configure: func(input *catalog.Catalog) {
				input.Runtimes[0].Spec.Environment = map[string]string{"TOKEN": "public"}
				addRuntimeCredential(input, "backend", catalog.CredentialInjection{
					Environment: map[string]catalog.CredentialEnvironmentVariable{
						"TOKEN": {Key: "token"},
					},
				})
			},
			wantError: "conflicts with runtime environment",
		},
		{
			name: "relative file path",
			configure: func(input *catalog.Catalog) {
				addRuntimeCredential(input, "backend", catalog.CredentialInjection{
					Files: []catalog.CredentialFile{{Key: "token", Path: "token"}},
				})
			},
			wantError: "clean absolute path",
		},
		{
			name: "invalid file mode",
			configure: func(input *catalog.Catalog) {
				addRuntimeCredential(input, "backend", catalog.CredentialInjection{
					Files: []catalog.CredentialFile{{
						Key: "token", Path: "/token", Mode: 0o1000,
					}},
				})
			},
			wantError: "outside 0777",
		},
		{
			name: "overlapping mounts",
			configure: func(input *catalog.Catalog) {
				addRuntimeCredential(input, "backend", catalog.CredentialInjection{
					Files: []catalog.CredentialFile{{
						Key: "token", Path: "/var/run/backend/token",
					}},
					Directory: &catalog.CredentialDirectory{Path: "/var/run/backend"},
				})
			},
			wantError: "conflicts with",
		},
		{
			name: "missing binding",
			configure: func(input *catalog.Catalog) {
				input.Runtimes[0].Spec.Credentials = map[string]catalog.RuntimeCredential{
					"backend": {
						Inject: catalog.CredentialInjection{EnvironmentFrom: true},
					},
				}
			},
			wantError: `must bind runtime credential "backend"`,
		},
		{
			name: "binding source not offered by placement",
			configure: func(input *catalog.Catalog) {
				addRuntimeCredential(input, "backend", catalog.CredentialInjection{EnvironmentFrom: true})
				input.Targets[0].Spec.Credentials["backend"] = "missing-source"
			},
			wantError: "not offered by placement",
		},
		{
			name: "unused target binding",
			configure: func(input *catalog.Catalog) {
				input.Targets[0].Spec.Credentials = map[string]string{
					"unused": "credential-source",
				}
			},
			wantError: "not declared by any selected runtime",
		},
		{
			name: "undeclared action binding",
			configure: func(input *catalog.Catalog) {
				action := input.Targets[0].Spec.Actions["preview"]
				action.Credentials = map[string]string{
					"unused": "credential-source",
				}
				input.Targets[0].Spec.Actions["preview"] = action
			},
			wantError: "not declared by runtime",
		},
	}

	runValidationTests(t, tests)
}

func addRuntimeCredential(input *catalog.Catalog, name string, injection catalog.CredentialInjection) {
	input.Runtimes[0].Spec.Credentials = map[string]catalog.RuntimeCredential{
		name: {Inject: injection},
	}
	input.Targets[0].Spec.Credentials = map[string]string{
		name: "credential-source",
	}
}
