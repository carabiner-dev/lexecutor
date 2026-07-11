// SPDX-FileCopyrightText: Copyright 2026 Carabiner Systems, Inc
// SPDX-License-Identifier: Apache-2.0

package lexecutor

import (
	"path/filepath"
	"testing"
)

// RunAllTests discovers test suites under rootDir and runs each test case
// as a Go subtest against HEAD (library), stable, and eol (downloaded or
// built from the two most recent upstream tags).
func RunAllTests(t *testing.T, rootDir string) {
	t.Helper()

	runners := []VersionRunner{&HeadRunner{}}

	bins, err := GetAmpelBinaries()
	if err != nil {
		t.Logf("could not get versioned ampel binaries, running HEAD only: %v", err)
	} else {
		t.Cleanup(bins.Cleanup)
		runners = append(runners,
			&BinaryRunner{Name: "stable (" + bins.StableTag + ")", BinaryPath: bins.Stable},
			&BinaryRunner{Name: "eol (" + bins.EOLTag + ")", BinaryPath: bins.EOL},
		)
	}

	RunAllTestsWithRunners(t, rootDir, runners)
}

// RunAllTestsWithRunners discovers suites and runs them against the provided
// version runners.
func RunAllTestsWithRunners(t *testing.T, rootDir string, runners []VersionRunner) {
	t.Helper()

	suites, err := Discover(rootDir)
	if err != nil {
		t.Fatalf("discovering test suites: %v", err)
	}

	if len(suites) == 0 {
		t.Fatal("no .ptests.yaml files found")
	}

	// Precompute each policy's runtime/plugin requirements once (shared across
	// all runners), keyed by resolved policy path.
	requirements := map[string]map[string]string{}
	for _, ds := range suites {
		for i := range ds.Suite.Tests {
			path := resolvePath(ds.Dir, ds.Suite.Tests[i].Policy)
			if _, ok := requirements[path]; ok {
				continue
			}
			req, err := policyRequiredPlugins(path)
			if err != nil {
				t.Fatalf("reading runtime requirements for %s: %v", path, err)
			}
			requirements[path] = req
		}
	}

	for _, runner := range runners {
		runner := runner
		t.Run(runner.Version(), func(t *testing.T) {
			t.Parallel()
			for _, ds := range suites {
				ds := ds
				relDir, err := filepath.Rel(rootDir, ds.Dir)
				if err != nil {
					relDir = ds.Dir
				}
				t.Run(relDir, func(t *testing.T) {
					t.Parallel()
					for i := range ds.Suite.Tests {
						tc := ds.Suite.Tests[i]
						t.Run(tc.Name, func(t *testing.T) {
							t.Parallel()

							// Skip tests whose policy needs plugins this runner
							// can't provide (e.g. an older binary that predates
							// the plugin). Runners that carry the plugin run the
							// test normally.
							required := requirements[resolvePath(ds.Dir, tc.Policy)]
							if len(required) > 0 && !runner.SupportsRuntimeRequirements(t.Context()) {
								t.Skipf("%s cannot provide plugins required by %s: %v", runner.Version(), tc.Policy, required)
							}

							result, err := runner.RunTest(t.Context(), ds.Dir, &tc)
							if err != nil {
								t.Fatalf("runner error: %v", err)
							}
							if result.Error != nil {
								t.Fatalf("test error: %v", result.Error)
							}

							// A capable runner that still can't satisfy the
							// policy soft-fails it; treat that as a skip rather
							// than asserting the PASS/FAIL expectation.
							if result.Actual == "SOFTFAIL" && tc.Expect != "SOFTFAIL" {
								t.Skipf("%s skipped %s (unsupported runtime): %v", runner.Version(), tc.Policy, required)
							}
							if !result.Passed {
								t.Errorf("expected %s, got %s", result.Expected, result.Actual)
							}
						})
					}
				})
			}
		})
	}
}

// resolvePath resolves a policy/attestation path from a suite against its base
// directory, leaving absolute paths untouched.
func resolvePath(baseDir, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(baseDir, p)
}
