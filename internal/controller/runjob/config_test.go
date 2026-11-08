package runjob

func validConfig() Config {
	return Config{
		ClusterName:         "colo",
		ServerURL:           "https://slipramp.example.com",
		CheckoutImage:       "registry.example.com/slipramp-checkout:1",
		HarnessImage:        "registry.example.com/slipramp-harness:1",
		ExecutionNamespaces: []string{"slipramp-runs"},
		GitCredentials: map[string]string{
			"github-readonly": "github-readonly",
		},
	}
}
