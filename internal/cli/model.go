package cli

import (
	"strings"

	"github.com/commoddity/discursive/internal/gateway"
)

func completeModelIDs(toComplete string) []string {
	var out []string
	for _, m := range gateway.ListAdvertisedModels() {
		if toComplete == "" || strings.HasPrefix(m.ID, toComplete) {
			out = append(out, m.ID)
		}
	}
	return out
}
