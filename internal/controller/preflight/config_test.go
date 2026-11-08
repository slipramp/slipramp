package preflight

func validConfig() Config {
	return Config{
		ClusterName:         "colo",
		CheckoutImage:       "registry.example.com/slipramp-checkout:1",
		ExecutionNamespaces: []string{"slipramp-runs"},
	}
}
