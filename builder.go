// SPDX-FileCopyrightText: Copyright 2026 Carabiner Systems, Inc
// SPDX-License-Identifier: Apache-2.0

package lexecutor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/mod/semver"
)

const defaultAmpelRepo = "https://github.com/carabiner-dev/ampel.git"

// AmpelBuilds holds the paths to ampel binaries built from specific tags.
type AmpelBuilds struct {
	// Stable is the binary built from the latest stable tag.
	Stable string
	// EOL is the binary built from the second-latest stable tag.
	EOL string
	// dir is the temp directory holding the clone and binaries.
	dir string
}

// Cleanup removes the temporary directory with the clone and binaries.
func (b *AmpelBuilds) Cleanup() {
	if b.dir != "" {
		os.RemoveAll(b.dir)
	}
}

// BuildAmpelVersions clones the ampel repo, finds the two most recent stable
// tags, and builds a binary from each. Returns the paths to both binaries.
// Call Cleanup() on the result when done.
func BuildAmpelVersions() (*AmpelBuilds, error) {
	return BuildAmpelVersionsFrom(defaultAmpelRepo)
}

// BuildAmpelVersionsFrom is like BuildAmpelVersions but clones from a custom
// repo URL or local path.
func BuildAmpelVersionsFrom(repo string) (*AmpelBuilds, error) {
	dir, err := os.MkdirTemp("", "lexecutor-ampel-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}

	builds := &AmpelBuilds{dir: dir}

	cloneDir := filepath.Join(dir, "ampel")
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		builds.Cleanup()
		return nil, err
	}

	// Clone the repo
	if err := run(dir, "git", "clone", "--quiet", repo, cloneDir); err != nil {
		builds.Cleanup()
		return nil, fmt.Errorf("cloning ampel: %w", err)
	}

	// List tags and find the two most recent stable ones
	tags, err := listStableTags(cloneDir)
	if err != nil {
		builds.Cleanup()
		return nil, err
	}
	if len(tags) < 2 {
		builds.Cleanup()
		return nil, fmt.Errorf("need at least 2 stable tags, found %d", len(tags))
	}

	stableTag := tags[0]
	eolTag := tags[1]

	// Build stable binary
	stableBin := filepath.Join(binDir, "ampel-stable")
	if err := buildAtTag(cloneDir, stableTag, stableBin); err != nil {
		builds.Cleanup()
		return nil, fmt.Errorf("building stable (%s): %w", stableTag, err)
	}
	builds.Stable = stableBin

	// Build eol binary
	eolBin := filepath.Join(binDir, "ampel-eol")
	if err := buildAtTag(cloneDir, eolTag, eolBin); err != nil {
		builds.Cleanup()
		return nil, fmt.Errorf("building eol (%s): %w", eolTag, err)
	}
	builds.EOL = eolBin

	return builds, nil
}

// listStableTags returns stable semver tags (vX.Y.Z, no pre-release) sorted
// descending. At least 2 are required.
func listStableTags(repoDir string) ([]string, error) {
	out, err := output(repoDir, "git", "tag", "--list", "v*")
	if err != nil {
		return nil, fmt.Errorf("listing tags: %w", err)
	}

	var tags []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		tag := strings.TrimSpace(line)
		if tag == "" {
			continue
		}
		// Only stable releases: vX.Y.Z with no pre-release suffix
		if !semver.IsValid(tag) {
			continue
		}
		if semver.Prerelease(tag) != "" {
			continue
		}
		tags = append(tags, tag)
	}

	sort.Slice(tags, func(i, j int) bool {
		return semver.Compare(tags[i], tags[j]) > 0 // descending
	})

	return tags, nil
}

// buildAtTag checks out a tag and builds the ampel binary.
func buildAtTag(repoDir, tag, outputPath string) error {
	if err := run(repoDir, "git", "checkout", "--quiet", tag); err != nil {
		return fmt.Errorf("checking out %s: %w", tag, err)
	}
	if err := run(repoDir, "go", "build", "-o", outputPath, "./cmd/ampel"); err != nil {
		return fmt.Errorf("building at %s: %w", tag, err)
	}
	return nil
}

func run(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func output(dir string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}
