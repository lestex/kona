// Package version exposes build information and the pinned component
// matrix. All values are injected at link time from versions.env; see the
// Makefile and .goreleaser.yaml.
package version

import (
	"sort"
	"strings"
)

var (
	// Version is the kona release version.
	Version = "dev"
	// Commit is the git commit kona was built from.
	Commit = "unknown"
	// matrix is versions.env flattened to "KEY=VALUE,KEY=VALUE".
	matrix = ""
	// checksums holds the *_SHA256* entries of versions.env, same format.
	// Kept out of the matrix so `kona version` stays readable.
	checksums = ""
)

// Checksum returns the pinned sha256 for key (e.g.
// "CILIUM_CLI_SHA256_DARWIN_ARM64") or "".
func Checksum(key string) string {
	for _, c := range parse(checksums) {
		if c.Name == key {
			return c.Version
		}
	}
	return ""
}

// Component is one pinned dependency.
type Component struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Matrix returns the pinned component versions, sorted by name.
func Matrix() []Component {
	return parse(matrix)
}

// Get returns the pinned version of key (e.g. "KUBERNETES_VERSION") or "".
func Get(key string) string {
	for _, c := range Matrix() {
		if c.Name == key {
			return c.Version
		}
	}
	return ""
}

func parse(s string) []Component {
	var out []Component
	for kv := range strings.SplitSeq(s, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(kv), "=")
		if !ok || k == "" {
			continue
		}
		out = append(out, Component{Name: k, Version: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
