package cli

import (
	"encoding/json"
	"fmt"
	"os"
)

// emitPretty writes v as indented JSON to stdout followed by a newline.
// Use for structured CLI output instead of compact slog JSON lines.
func emitPretty(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("emit pretty json: %w", err)
	}
	return nil
}
