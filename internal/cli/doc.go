// Package cli implements the Discursive Cobra command tree.
//
// Subcommands live in internal/cli/<name>/ (initcmd, start, stop, status, setcmd,
// logs, doctor, usage, loglevel). Shared helpers are in internal/cli/util/ and
// background-process helpers in internal/cli/daemon/.
//
// Contract: owns CLI UX and wires internal/config, gateway, tunnel, doctor,
// usage. Binary entry is main.go at repo root (thin main → Execute).
package cli
