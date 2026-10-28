package main

import (
	"testing"

	"github.com/alkiranet/terraform-provider-alkira/alkira"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// TestCatalogMatchesProviderOnRampRegionFormat: every on-ramp region the sandbox accepts
// passes the provider's plan-time on_ramp_region check, so the two never disagree.
func TestCatalogMatchesProviderOnRampRegionFormat(t *testing.T) {
	r := alkira.Provider().ResourcesMap["alkira_connector_multi_cloud_gateway"]
	validate := r.Schema["instance"].Elem.(*schema.Resource).Schema["on_ramp_region"].ValidateFunc
	for _, region := range onRampRegions {
		if _, errs := validate(region.Name, "on_ramp_region"); len(errs) != 0 {
			t.Errorf("sandbox region %q fails provider validation: %v", region.Name, errs)
		}
	}
}
