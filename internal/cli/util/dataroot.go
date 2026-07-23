package util

import "github.com/commoddity/discursive/internal/config"

// ResolveDataRoot ensures and returns the app data directory.
// portable mirrors the root --portable persistent flag.
func ResolveDataRoot(portable bool) (string, error) {
	opts, err := config.DefaultResolveOpts(portable)
	if err != nil {
		return "", err
	}
	return config.EnsureDataRoot(opts)
}
