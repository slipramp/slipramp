// Package kargo observes Kargo Stages and manages Kargo Promotions through
// Kubernetes dynamic clients.
package kargo

import "github.com/slipramp/slipramp/internal/promotion"

// Config contains the configured Kargo integrations available to this
// Controller.
type Config struct {
	Integrations []promotion.ObserverConfig
}
