// Command format reformats the provider config JSON files in place, using
// the jsonfmt layout: like encoding/json's MarshalIndent, but with objects
// and arrays that fit within 80 columns collapsed onto a single line. Run it
// after any generator (or manual edit) to keep every config consistent.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"charm.land/catwalk/v3/internal/jsonfmt"
)

const configsDir = "internal/providers/configs"

func main() {
	dir := configsDir
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to read directory %s: %v\n", dir, err)
		os.Exit(1)
	}
	changed := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".json" {
			continue
		}
		path := filepath.Join(dir, name)
		if reformat(path) {
			changed++
			fmt.Println("reformatted", path)
		}
	}
	fmt.Printf("done: %d/%d files reformatted\n", changed, len(entries))
}

func reformat(path string) bool {
	raw, err := os.ReadFile(path) //nolint:gosec // operator-provided path
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to read %s: %v\n", path, err)
		os.Exit(1)
	}
	out := jsonfmt.Format(raw, jsonfmt.Options{})
	if string(out) == string(raw) {
		return false
	}
	info, err := os.Stat(path) //nolint:gosec // operator-provided path
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to stat %s: %v\n", path, err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, out, info.Mode().Perm()); err != nil { //nolint:gosec // operator-provided path
		fmt.Fprintf(os.Stderr, "unable to write %s: %v\n", path, err)
		os.Exit(1)
	}
	return true
}
