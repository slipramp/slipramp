package placement

import (
	"context"
	"maps"
	"slices"
	"time"

	"github.com/slipramp/slipramp/internal/cluster"
)

const defaultServiceAccountName = "default"

type ClusterInventory interface {
	ListClusters(ctx context.Context) ([]cluster.Status, error)
}

type InventoryService struct {
	placements Set
	clusters   ClusterInventory
}

type Inventory struct {
	Items                 []InventoryPlacement
	ConfigurationChecksum string
}

type InventoryPlacement struct {
	Name                      string
	ClusterName               string
	ControllerConnectionState cluster.ConnectionState
	ControllerLastSeenAt      *time.Time
	JobNamespace              string
	ServiceAccountName        string
	Timeout                   *time.Duration
	Resources                 ResourceRequirements
	CredentialNames           []string
}

func NewInventoryService(placements Set, clusters ClusterInventory) *InventoryService {
	return &InventoryService{placements: placements, clusters: clusters}
}

func (s *InventoryService) Inventory(ctx context.Context) (Inventory, error) {
	clusterStatuses, err := s.clusters.ListClusters(ctx)
	if err != nil {
		return Inventory{}, err
	}

	statusesByName := make(map[string]cluster.Status, len(clusterStatuses))
	for _, status := range clusterStatuses {
		statusesByName[status.Name] = status
	}

	items := make([]InventoryPlacement, 0, s.placements.Len())
	for _, name := range s.placements.Names() {
		cfg, _ := s.placements.Lookup(name)
		serviceAccountName := cfg.ServiceAccountName
		if serviceAccountName == "" {
			serviceAccountName = defaultServiceAccountName
		}

		item := InventoryPlacement{
			Name:                      name,
			ClusterName:               cfg.ClusterName,
			ControllerConnectionState: cluster.ConnectionStateUnreachable,
			JobNamespace:              cfg.JobNamespace,
			ServiceAccountName:        serviceAccountName,
			Resources: ResourceRequirements{
				Requests: cloneInventoryMap(cfg.Resources.Requests),
				Limits:   cloneInventoryMap(cfg.Resources.Limits),
			},
			CredentialNames: slices.Sorted(maps.Keys(cfg.Credentials)),
		}
		if cfg.Timeout > 0 {
			item.Timeout = new(cfg.Timeout)
		}
		if status, found := statusesByName[cfg.ClusterName]; found {
			item.ControllerConnectionState = status.ConnectionState
			item.ControllerLastSeenAt = new(status.LastSeenAt)
		}

		items = append(items, item)
	}

	return Inventory{Items: items, ConfigurationChecksum: s.placements.Checksum()}, nil
}

func cloneInventoryMap(values map[string]string) map[string]string {
	cloned := maps.Clone(values)
	if cloned == nil {
		return map[string]string{}
	}

	return cloned
}
