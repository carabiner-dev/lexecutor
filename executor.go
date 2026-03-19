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
