package util

import (
	"strings"

	"github.com/commoddity/discursive/internal/gateway"
)

// CompleteModelIDs returns advertised model IDs matching toComplete (shell completion).
func CompleteModelIDs(toComplete string) []string {
	var out []string
	for _, m := range gateway.ListAdvertisedModels() {
		if toComplete == "" || strings.HasPrefix(m.ID, toComplete) {
			out = append(out, m.ID)
		}
	}
	return out
}
