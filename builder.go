// SPDX-FileCopyrightText: Copyright 2026 Carabiner Systems, Inc
// SPDX-License-Identifier: Apache-2.0

package lexecutor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"golang.org/x/mod/semver"
)

const (
	defaultAmpelRepo  = "https://github.com/carabiner-dev/ampel.git"
	defaultAmpelOwner = "carabiner-dev"
	defaultAmpelName  = "ampel"
)

// AmpelBinaries holds the paths to ampel binaries for specific versions.
type AmpelBinaries struct {
	// Stable is the binary for the latest stable tag.
	Stable string
	// StableTag is the tag used for the stable binary.
	StableTag string
	// EOL is the binary for the second-latest stable tag.
	EOL string
	// EOLTag is the tag used for the eol binary.
	EOLTag string
	// dir is the temp directory holding the binaries.
	dir string
}

// Cleanup removes the temporary directory with the binaries.
func (b *AmpelBinaries) Cleanup() {
	if b.dir != "" {
		os.RemoveAll(b.dir)
	}
}

// GetAmpelBinaries fetches ampel binaries for the two most recent stable tags.
// It downloads pre-built binaries from GitHub releases. If that fails, it
// falls back to cloning and building from source.
func GetAmpelBinaries() (*AmpelBinaries, error) {
	tags, err := listReleaseTags()
	if err != nil {
		return nil, fmt.Errorf("listing release tags: %w", err)
	}
	if len(tags) < 2 {
		return nil, fmt.Errorf("need at least 2 stable releases, found %d", len(tags))
	}

	stableTag := tags[0]
	eolTag := tags[1]

	dir, err := os.MkdirTemp("", "lexecutor-ampel-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	bins := &AmpelBinaries{dir: dir, StableTag: stableTag, EOLTag: eolTag}

	// Try downloading pre-built binaries
	stableBin := filepath.Join(dir, "ampel-stable")
	if err := downloadReleaseBinary(stableTag, stableBin); err != nil {
		bins.Cleanup()
		return buildAmpelBinaries(stableTag, eolTag)
	}
	bins.Stable = stableBin

	eolBin := filepath.Join(dir, "ampel-eol")
	if err := downloadReleaseBinary(eolTag, eolBin); err != nil {
		bins.Cleanup()
		return buildAmpelBinaries(stableTag, eolTag)
	}
	bins.EOL = eolBin

	return bins, nil
}

// listReleaseTags uses gh CLI to list stable release tags, sorted descending.
func listReleaseTags() ([]string, error) {
	out, err := exec.Command(
		"gh", "release", "list",
		"-R", defaultAmpelOwner+"/"+defaultAmpelName,
		"--exclude-drafts", "--exclude-pre-releases",
		"--json", "tagName",
		"--jq", ".[].tagName",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("gh release list: %w", err)
	}

	var tags []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		tag := strings.TrimSpace(line)
		if tag != "" && semver.IsValid(tag) {
			tags = append(tags, tag)
		}
	}

	sort.Slice(tags, func(i, j int) bool {
		return semver.Compare(tags[i], tags[j]) > 0
	})
	return tags, nil
}

// downloadReleaseBinary downloads the ampel binary for the given tag using gh.
func downloadReleaseBinary(tag, outputPath string) error {
	assetName := fmt.Sprintf("ampel-%s-%s-%s", tag, runtime.GOOS, runtime.GOARCH)

	tmpDir, err := os.MkdirTemp("", "lexecutor-dl-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	cmd := exec.Command(
		"gh", "release", "download", tag,
		"-R", defaultAmpelOwner+"/"+defaultAmpelName,
		"-p", assetName,
		"-D", tmpDir,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}

	downloaded := filepath.Join(tmpDir, assetName)
	if err := os.Chmod(downloaded, 0o755); err != nil {
		return err
	}

	return os.Rename(downloaded, outputPath)
}

// buildAmpelBinaries clones the repo and builds binaries from the given tags.
func buildAmpelBinaries(stableTag, eolTag string) (*AmpelBinaries, error) {
	dir, err := os.MkdirTemp("", "lexecutor-ampel-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}

	bins := &AmpelBinaries{dir: dir, StableTag: stableTag, EOLTag: eolTag}
	cloneDir := filepath.Join(dir, "ampel")
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		bins.Cleanup()
		return nil, err
	}

	if err := runCmd(dir, "git", "clone", "--quiet", defaultAmpelRepo, cloneDir); err != nil {
		bins.Cleanup()
		return nil, fmt.Errorf("cloning ampel: %w", err)
	}

	stableBin := filepath.Join(binDir, "ampel-stable")
	if err := buildAtTag(cloneDir, stableTag, stableBin); err != nil {
		bins.Cleanup()
		return nil, fmt.Errorf("building stable (%s): %w", stableTag, err)
	}
	bins.Stable = stableBin

	eolBin := filepath.Join(binDir, "ampel-eol")
	if err := buildAtTag(cloneDir, eolTag, eolBin); err != nil {
		bins.Cleanup()
		return nil, fmt.Errorf("building eol (%s): %w", eolTag, err)
	}
	bins.EOL = eolBin

	return bins, nil
}

// buildAtTag checks out a tag and builds the ampel binary.
func buildAtTag(repoDir, tag, outputPath string) error {
	if err := runCmd(repoDir, "git", "checkout", "--quiet", tag); err != nil {
		return fmt.Errorf("checking out %s: %w", tag, err)
	}
	if err := runCmd(repoDir, "go", "build", "-o", outputPath, "./cmd/ampel"); err != nil {
		return fmt.Errorf("building at %s: %w", tag, err)
	}
	return nil
}

func runCmd(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
