// Package cli implements the Discursive Cobra command tree
// (init, start, stop, status, doctor, usage, key/tunnel setters).
//
// Contract: owns CLI UX and wires internal/config, gateway, tunnel, doctor,
// usage. Binary entry is main.go at repo root (thin main → Execute).
package cli
