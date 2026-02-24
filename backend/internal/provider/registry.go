package provider

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shanq/tardi/internal/db"
	"github.com/shanq/tardi/internal/models"
)

type Registry struct {
	providers map[string]InfraProvider
}

func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]InfraProvider),
	}
}

func (r *Registry) Register(name string, p InfraProvider) {
	r.providers[name] = p
}

func (r *Registry) Get(name string) (InfraProvider, error) {
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider not registered: %s", name)
	}
	return p, nil
}

// SelectProvider returns the best provider mapping and its implementation for a plan/region.
func (r *Registry) SelectProvider(ctx context.Context, pool *pgxpool.Pool, planTier models.PlanTier, region string) (*models.ProviderPlanMapping, InfraProvider, error) {
	mapping, err := db.GetBestProviderMapping(ctx, pool, planTier, region)
	if err != nil {
		return nil, nil, fmt.Errorf("get provider mapping: %w", err)
	}

	p, err := r.Get(mapping.Provider)
	if err != nil {
		return nil, nil, fmt.Errorf("provider %s not registered: %w", mapping.Provider, err)
	}

	return mapping, p, nil
}
