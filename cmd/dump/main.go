// Package main provides a CLI that dumps all registered providers as JSON to
// stdout. It is used by the static hosting build (e.g. Vercel) to generate the
// providers file served under /v3/providers.json.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"charm.land/catwalk/internal/providers"
)

func main() {
	data, err := json.MarshalIndent(providers.GetAll(), "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to marshal providers:", err)
		os.Exit(1)
	}

	if _, err := os.Stdout.Write(append(data, '\n')); err != nil {
		fmt.Fprintln(os.Stderr, "Failed to write providers:", err)
		os.Exit(1)
	}
}
