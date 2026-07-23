package util

import (
	"encoding/json"
	"fmt"
	"os"
)

// EmitPretty writes v as indented JSON to stdout followed by a newline.
func EmitPretty(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("emit pretty json: %w", err)
	}
	return nil
}
