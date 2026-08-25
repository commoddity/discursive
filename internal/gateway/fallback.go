package gateway

import "github.com/commoddity/discursive/internal/config"

// openRouterTwinFor is the gateway wrapper around the provider-catalog OR map.
func openRouterTwinFor(model string) (string, bool) {
	return config.OpenRouterTwinFor(model)
}
