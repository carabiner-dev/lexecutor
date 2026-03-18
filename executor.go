// SPDX-FileCopyrightText: Copyright 2026 Carabiner Systems, Inc
// SPDX-License-Identifier: Apache-2.0

package lexecutor

import (
	"path/filepath"
	"testing"
)

// RunAllTests discovers test suites under rootDir and runs each test case
// as a Go subtest against HEAD (library), stable, and eol (binaries built
// from the two most recent upstream tags).
func RunAllTests(t *testing.T, rootDir string) {
	t.Helper()

	runners := []VersionRunner{&HeadRunner{}}

	builds, err := BuildAmpelVersions()
	if err != nil {
		t.Logf("WARNING: could not build versioned ampel binaries: %v", err)
		t.Log("Running HEAD only. Set AMPEL_STABLE_BIN / AMPEL_EOL_BIN to provide binaries manually.")
	} else {
		t.Cleanup(builds.Cleanup)
		runners = append(runners,
			&BinaryRunner{Name: "stable", BinaryPath: builds.Stable},
			&BinaryRunner{Name: "eol", BinaryPath: builds.EOL},
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
		t.Fatal("no .ampel-tests.yaml files found")
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
							result, err := runner.RunTest(t.Context(), ds.Dir, &tc)
							if err != nil {
								t.Fatalf("runner error: %v", err)
							}
							if result.Error != nil {
								t.Fatalf("test error: %v", result.Error)
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
