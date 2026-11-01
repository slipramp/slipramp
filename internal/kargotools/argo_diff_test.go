package kargotools

import (
	"reflect"
	"testing"

	"github.com/slipramp/slipramp/internal/argodiff"
)

func TestParseArgoDiff(t *testing.T) {
	t.Parallel()

	t.Run("builds deterministic resources", func(t *testing.T) {
		t.Parallel()

		output := []byte(`warning: comparison used cached manifests
===== apps/Deployment default/api ======
@@ -10,3 +10,3 @@
-  replicas: 1
+  replicas: 2

===== /Service default/api ======
12c12
<     app.kubernetes.io/version: v1.21.1
---
>     app.kubernetes.io/version: v1.21.2

===== rbac.authorization.k8s.io/ClusterRole /api-reader ======
8a9
> - list`)

		got, err := ParseArgoDiff(output)
		if err != nil {
			t.Fatal(err)
		}
		want := argodiff.Document{Resources: []argodiff.Resource{
			{
				Kind:      "Service",
				Namespace: "default",
				Name:      "api",
				Diff:      "12c12\n<     app.kubernetes.io/version: v1.21.1\n---\n>     app.kubernetes.io/version: v1.21.2",
			},
			{
				Group:     "apps",
				Kind:      "Deployment",
				Namespace: "default",
				Name:      "api",
				Diff:      "@@ -10,3 +10,3 @@\n-  replicas: 1\n+  replicas: 2",
			},
			{
				Group: "rbac.authorization.k8s.io",
				Kind:  "ClusterRole",
				Name:  "api-reader",
				Diff:  "8a9\n> - list",
			},
		}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ParseArgoDiff() = %#v, want %#v", got, want)
		}
	})

	for _, test := range []struct {
		name string
		diff string
	}{
		{
			name: "unified Service selector diff",
			diff: "@@ -5,3 +5,3 @@\n spec:\n   selector:\n" +
				"-    app.kubernetes.io/version: stable\n+    app.kubernetes.io/version: canary",
		},
		{
			name: "normal diff without field context",
			diff: "12c12\n<     helm.sh/chart: stable\n---\n>     helm.sh/chart: canary",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseArgoDiff([]byte("===== /Service default/api ======\n" + test.diff))
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Resources) != 1 || got.Resources[0].Diff != test.diff || got.Resources[0].VersionMetadataOnly {
				t.Fatalf("version-key change is not fully visible: %#v", got)
			}
		})
	}

	t.Run("returns an explicit empty result", func(t *testing.T) {
		t.Parallel()

		got, err := ParseArgoDiff([]byte("\n"))
		if err != nil {
			t.Fatal(err)
		}
		if got.Resources == nil || len(got.Resources) != 0 {
			t.Fatalf("empty Argo CD diff = %#v", got)
		}
	})

	t.Run("rejects unstructured output", func(t *testing.T) {
		t.Parallel()

		if _, err := ParseArgoDiff([]byte("No differences.")); err == nil {
			t.Fatal("ParseArgoDiff() accepted non-empty unstructured output")
		}
	})
}
