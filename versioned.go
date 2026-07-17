// SPDX-FileCopyrightText: Copyright 2026 Carabiner Systems, Inc
// SPDX-License-Identifier: Apache-2.0

package lexecutor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/carabiner-dev/policyctl/pkg/tester"
)

// VersionRunner can execute a test case against a specific ampel version.
type VersionRunner interface {
	Version() string
	RunTest(ctx context.Context, baseDir string, tc *tester.TestCase) (*tester.TestResult, error)

	// SupportsRuntimeRequirements reports whether this ampel understands policy
	// runtime/plugin requirements, i.e. it will run a policy that declares them
	// (when it has the plugin) or gracefully skip it (when it doesn't) rather
	// than fail with an undefined-function error. Tests whose policy declares
	// plugin requirements are skipped on runners that return false.
	SupportsRuntimeRequirements(ctx context.Context) bool

	// SupportsCollectors reports whether this runner can execute test cases that
	// declare `collectors:` — i.e. whether it can synthesize evidence (such as
	// signature attestations built from sigstore bundles or detached
	// signatures) by running a collector. Tests that declare collectors are
	// skipped on runners that return false.
	SupportsCollectors(ctx context.Context) bool
}

// HeadRunner uses the current (HEAD) ampel version via the Go library.
type HeadRunner struct{}

func (h *HeadRunner) Version() string { return "HEAD" }

func (h *HeadRunner) RunTest(ctx context.Context, baseDir string, tc *tester.TestCase) (*tester.TestResult, error) {
	runner := tester.NewRunner(baseDir)
	result := runner.RunTest(ctx, tc)
	return result, nil
}

// SupportsRuntimeRequirements is always true for HEAD: the library under test
// is the current development tree, which carries every plugin the policies in
// this repository target.
func (h *HeadRunner) SupportsRuntimeRequirements(context.Context) bool { return true }

// SupportsCollectors is always true for HEAD: the library under test is the
// current development tree, which carries the collector features the tests in
// this repository target.
func (h *HeadRunner) SupportsCollectors(context.Context) bool { return true }

// BinaryRunner shells out to an ampel binary for verification.
// This avoids Go module dependency conflicts when testing against
// older ampel versions.
type BinaryRunner struct {
	// Name is the version label shown in test output (e.g. "stable", "eol").
	Name string

	// BinaryPath is the path to the ampel binary to invoke.
	BinaryPath string

	// skipOnce guards the one-time --skip-unsupported-runtime capability probe.
	skipOnce sync.Once
	// skipSupported caches whether the binary advertises the flag.
	skipSupported bool
}

func (b *BinaryRunner) Version() string { return b.Name }

// SupportsRuntimeRequirements probes (once) whether the binary advertises the
// --skip-unsupported-runtime verify flag. Presence of that flag is our signal
// that this ampel version understands policy runtime/plugin requirements and
// will skip rather than choke on a policy whose plugins it lacks. Older
// binaries that predate the flag return false.
func (b *BinaryRunner) SupportsRuntimeRequirements(ctx context.Context) bool {
	b.skipOnce.Do(func() {
		out, err := exec.CommandContext(ctx, b.BinaryPath, "verify", "--help").CombinedOutput()
		b.skipSupported = err == nil && strings.Contains(string(out), "--skip-unsupported-runtime")
	})
	return b.skipSupported
}

// SupportsCollectors reports whether this ampel binary can run collector-based
// tests. Such tests need the binary's collector to synthesize the evidence
// (e.g. build a signature attestation from a sigstore bundle or a detached
// certificate + signature pair), and a binary whose collector predates that
// support silently returns no attestations, which surfaces as a spurious
// failure rather than a skip.
//
// Unlike plugin requirements, that capability has no CLI signal to probe (the
// --collector and --signer flags long predate it), so released binaries are
// gated out and collector-based tests run against HEAD only. Once binaries
// advertise the capability, probe for it here instead of returning false.
func (b *BinaryRunner) SupportsCollectors(context.Context) bool { return false }

func (b *BinaryRunner) RunTest(ctx context.Context, baseDir string, tc *tester.TestCase) (*tester.TestResult, error) {
	start := time.Now()
	result := &tester.TestResult{
		Name:     tc.Name,
		Expected: tc.Expect,
	}

	actual, err := b.exec(ctx, baseDir, tc)
	result.Duration = time.Since(start)
	if err != nil {
		result.Actual = "ERROR"
		result.Error = err
		//nolint:nilerr // the exec error is reported through result.Error, not the top-level return
		return result, nil
	}

	result.Actual = actual
	result.Passed = (actual == tc.Expect)
	return result, nil
}

func (b *BinaryRunner) exec(ctx context.Context, baseDir string, tc *tester.TestCase) (string, error) {
	resolve := func(p string) string {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(baseDir, p)
	}

	// Build args: ampel verify --subject-hash <subject> -p <policy> -a <att> [-x key:val...]
	args := []string{"verify", "--log-level", "error"}

	// When the binary understands runtime requirements, ask it to soft-fail
	// (skip) policies whose plugins it lacks instead of failing them. Harmless
	// for policies with no requirements. Not passed to older binaries that
	// wouldn't recognize the flag (they are gated out before RunTest instead).
	if b.SupportsRuntimeRequirements(ctx) {
		args = append(args, "--skip-unsupported-runtime")
	}

	// Subject
	if tc.Subject != "" {
		args = append(args, "--subject-hash", tc.Subject)
	} else {
		return "", fmt.Errorf("subject is required for binary runner")
	}

	// Policy
	args = append(args, "-p", resolve(tc.Policy))

	// Attestations
	for _, att := range tc.Attestations {
		args = append(args, "-a", resolve(att))
	}

	// Collectors (resolve fs: paths relative to the test dir)
	for _, col := range tc.Collectors {
		if moniker, rest, ok := strings.Cut(col, ":"); ok && moniker == "fs" && rest != "" && !filepath.IsAbs(rest) {
			col = moniker + ":" + resolve(rest)
		}
		args = append(args, "-c", col)
	}

	// Signer identities
	for _, signer := range tc.Signers {
		args = append(args, "--signer", signer)
	}

	// Context values as -x key:value
	for _, cv := range tc.Context {
		args = append(args, "-x", fmt.Sprintf("%s:%v", cv.Name, cv.Value))
	}

	// Context files
	for _, cf := range tc.ContextFiles {
		path := resolve(cf.Path)
		switch {
		case strings.HasSuffix(path, ".json"):
			args = append(args, "--context-json", "@"+path)
		case strings.HasSuffix(path, ".yaml"), strings.HasSuffix(path, ".yml"):
			args = append(args, "--context-yaml", "@"+path)
		}
	}

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, b.BinaryPath, args...)
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		// Exit code non-zero means FAIL
		var exitErr *exec.ExitError
		if ok := errors.As(err, &exitErr); ok {
			return "FAIL", nil
		}
		// Actual execution error (binary not found, etc.)
		return "", fmt.Errorf("running %s: %w\nstderr: %s", b.BinaryPath, err, strings.TrimSpace(stderr.String()))
	}

	return "PASS", nil
}
