// SPDX-FileCopyrightText: Copyright 2026 Carabiner Systems, Inc
// SPDX-License-Identifier: Apache-2.0

package lexecutor

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/carabiner-dev/policyctl/pkg/tester"
	"golang.org/x/mod/semver"
)

// recordingRunner is a VersionRunner with a fixed engine version that records
// the names of the test cases it was asked to run and reports every one of
// them as matching its expectation.
type recordingRunner struct {
	engine string

	mu  sync.Mutex
	ran []string
}

func (r *recordingRunner) Version() string                                  { return "fake (" + r.engine + ")" }
func (r *recordingRunner) EngineVersion() string                            { return r.engine }
func (r *recordingRunner) SupportsRuntimeRequirements(context.Context) bool { return true }
func (r *recordingRunner) SupportsCollectors(context.Context) bool          { return true }

func (r *recordingRunner) RunTest(_ context.Context, _ string, tc *tester.TestCase) (*tester.TestResult, error) {
	r.mu.Lock()
	r.ran = append(r.ran, tc.Name)
	r.mu.Unlock()
	return &tester.TestResult{Name: tc.Name, Expected: tc.Expect, Actual: tc.Expect, Passed: true}, nil
}

// writeFloorSuite writes a suite with one test case floored at v1.3.7 and one
// without a floor, both against a trivial policy, and returns its directory.
func writeFloorSuite(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	policy := `{"id": "FLOOR-TEST", "tenets": [{"id": "t01", "code": "true"}]}`
	if err := os.WriteFile(filepath.Join(dir, "policy.json"), []byte(policy), 0o600); err != nil {
		t.Fatalf("writing policy: %v", err)
	}

	suite := `tests:
  - name: floored
    policy: policy.json
    ampel-version: v1.3.7
    expect: PASS
    subject: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    attestations:
      - att.json
  - name: unfloored
    policy: policy.json
    expect: PASS
    subject: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    attestations:
      - att.json
`
	if err := os.WriteFile(filepath.Join(dir, ".ptests.yaml"), []byte(suite), 0o600); err != nil {
		t.Fatalf("writing suite: %v", err)
	}
	return dir
}

func TestRunAllTestsWithRunnersSkipsBelowVersionFloor(t *testing.T) {
	t.Parallel()
	dir := writeFloorSuite(t)

	for _, tt := range []struct {
		name   string
		engine string
		want   []string
	}{
		{name: "older engine skips the floored test", engine: "v1.3.6", want: []string{"unfloored"}},
		{name: "engine at the floor runs both", engine: "v1.3.7", want: []string{"floored", "unfloored"}},
		{name: "newer engine runs both", engine: "v1.4.0", want: []string{"floored", "unfloored"}},
		{name: "pseudo-version below the floor skips", engine: "v1.3.2-0.20260711022321-f6654bd33361", want: []string{"unfloored"}},
		{name: "unknown engine runs both", engine: "", want: []string{"floored", "unfloored"}},
		{name: "devel engine runs both", engine: "(devel)", want: []string{"floored", "unfloored"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner := &recordingRunner{engine: tt.engine}

			// The harness registers parallel subtests, which only execute
			// after this function returns, so check what ran once the whole
			// subtree has completed.
			t.Cleanup(func() {
				runner.mu.Lock()
				defer runner.mu.Unlock()
				slices.Sort(runner.ran)
				if !slices.Equal(runner.ran, tt.want) {
					t.Errorf("engine %q ran %v, want %v", tt.engine, runner.ran, tt.want)
				}
			})

			RunAllTestsWithRunners(t, dir, []VersionRunner{runner})
		})
	}
}

func TestHeadRunnerEngineVersion(t *testing.T) {
	t.Parallel()
	h := &HeadRunner{}
	v := h.EngineVersion()

	// The test binary links ampel through policyctl's tester, so build info
	// must report its module version: a tag or pseudo-version, or "(devel)"
	// when a replace directive points at a local checkout.
	if v == "" {
		t.Fatal("expected the linked ampel module version, got empty")
	}
	if v != "(devel)" && !semver.IsValid(v) {
		t.Fatalf("expected a semantic version or (devel), got %q", v)
	}
	if again := h.EngineVersion(); again != v {
		t.Fatalf("expected a stable version across calls, got %q then %q", v, again)
	}
}

func TestBinaryRunnerEngineVersion(t *testing.T) {
	t.Parallel()
	b := &BinaryRunner{Name: "stable (v1.3.7)", Tag: "v1.3.7", BinaryPath: "/nonexistent"}
	if got := b.EngineVersion(); got != "v1.3.7" {
		t.Fatalf("expected v1.3.7, got %q", got)
	}
	untagged := &BinaryRunner{Name: "custom", BinaryPath: "/nonexistent"}
	if got := untagged.EngineVersion(); got != "" {
		t.Fatalf("expected empty version for an untagged binary, got %q", got)
	}
}
