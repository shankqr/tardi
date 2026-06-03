package jobs

import (
	"testing"

	"github.com/shanq/tardi/internal/models"
	"github.com/shanq/tardi/internal/provider"
)

func TestServerAlreadyOnMapping(t *testing.T) {
	mapping := &models.ProviderPlanMapping{ProviderServerType: "ccx23"}

	if !serverAlreadyOnMapping(&provider.Server{ServerType: "ccx23"}, mapping) {
		t.Fatal("expected matching server type to be treated as already upgraded")
	}
	if serverAlreadyOnMapping(&provider.Server{ServerType: "cx23"}, mapping) {
		t.Fatal("expected different server type to require upgrade")
	}
	if serverAlreadyOnMapping(&provider.Server{}, mapping) {
		t.Fatal("expected missing server type to require upgrade")
	}
	if serverAlreadyOnMapping(nil, mapping) {
		t.Fatal("expected nil server to require upgrade")
	}
	if serverAlreadyOnMapping(&provider.Server{ServerType: "ccx23"}, nil) {
		t.Fatal("expected nil mapping to require upgrade")
	}
}
