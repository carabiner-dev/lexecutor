<!--
SPDX-FileCopyrightText: Copyright 2026 Carabiner Systems, Inc
SPDX-License-Identifier: Apache-2.0
-->

# lexecutor

`lexecutor` is a Go test harness that runs [AMPEL][ampel] policy test suites
against **multiple ampel versions at once**: the current development tree (via
the ampel Go library) plus the two most recent stable ampel releases (via
downloaded binaries). It lets a policy repository verify that its policies keep
producing the expected `PASS`/`FAIL` verdicts across the versions of ampel that
consumers are actually running.

It is the engine behind the test suite in the community
[policies][policies] repository, but it works with any directory tree of
policies and test definitions.

## How it works

Point `lexecutor` at a directory and it will:

1. **Discover** every `.ptests.yaml` suite under that directory.
2. **Resolve runners** — one per ampel version:
   - `HEAD`: the ampel version compiled into your test binary, exercised through
     the Go library (`policyctl`'s tester).
   - `stable` / `eol`: the two most recent stable ampel release tags. Their
     binaries are downloaded from GitHub releases (falling back to building from
     source), then invoked as subprocesses. This needs the [`gh`][gh] CLI to be
     available and authenticated.
3. **Run** every test case in every suite against every runner, as parallel Go
   subtests, and assert each verdict matches the declared expectation.

If the versioned binaries can't be resolved (for example `gh` is unavailable),
the harness logs a warning and runs against `HEAD` only.

## Usage

Add a single Go test that hands your policy tree to `lexecutor`:

```go
package policies_test

import (
	"testing"

	"github.com/carabiner-dev/lexecutor"
)

func TestPolicies(t *testing.T) {
	lexecutor.RunAllTests(t, ".")
}
```

```console
go test ./...
```

To run against `HEAD` only (or a custom set of versions), call
`RunAllTestsWithRunners` with your own `[]VersionRunner` instead.

## Test suites: `.ptests.yaml`

Each `.ptests.yaml` file declares a list of test cases. Paths are resolved
relative to the file's directory.

```yaml
tests:
  - name: no-critical-passes-when-only-medium-present
    policy: osv-no-critical.hjson          # policy or policy set to evaluate
    expect: PASS                           # PASS or FAIL
    subject: "sha256:7950b24d0640..."      # subject digest (algo:hex)
    attestations:
      - ../testdata/osv/no-critical.intoto.json
    context:                               # optional context values
      - name: max_severity
        value: high
    context-files:                         # optional context providers
      - path: ../testdata/context.json
    ampel-version: v1.3.7                  # optional: minimum ampel version
```

## Runtime requirements and version skipping

A policy can declare that it needs a specific evaluator engine version or CEL
plugin through its runtime specifier, e.g.:

```jsonc
meta: {
  runtime: "cel@v1?plugin:osv=v1"
}
```

Older ampel releases that predate a plugin can't evaluate such a policy. For
each test, `lexecutor` reads the plugin requirements declared by the policy and
**skips** the test on any runner that can't provide them, rather than reporting
a spurious failure:

- `HEAD` (the development tree) is assumed to carry every plugin the repository's
  policies target, so it always runs them.
- A released binary is considered capable only if its `ampel verify --help`
  advertises `--skip-unsupported-runtime`. Capable binaries are asked to
  soft-fail (skip) unmet policies via that flag; a `SOFTFAIL` verdict is
  reported as a skip. Binaries that predate the flag have the test skipped
  before it runs.

Policies with no plugin requirements run on every runner, unchanged.

## Version floors: `ampel-version`

Some behavior changes with the ampel release rather than with a plugin, for
example a bug fix in a bundled library that flips a policy's verdict. A test
case can declare the minimum ampel version it needs:

```yaml
  - name: producers-passes-with-cyclonedx-suppliers
    policy: protobom-sbom-producers.hjson
    ampel-version: v1.3.7
    expect: PASS
```

`lexecutor` compares that floor against each runner's engine version and
**skips** the test on older engines instead of failing it:

- `stable` / `eol` report the release tag their binary was built from.
- `HEAD` reports the ampel module version linked into the test binary, read
  from its build info. Pseudo-versions compare as semver, so a build of a
  commit between two tags sorts between them. When the version is unknown
  (for example a `replace` directive pointing at a local checkout reports
  `(devel)`), `HEAD` runs every test.

Since `eol` is always the second most recent release, a floored test starts
running on both released binaries as soon as the next ampel version ships.

## License

Apache-2.0. See [LICENSE](./LICENSE).

[ampel]: https://github.com/carabiner-dev/ampel
[policies]: https://github.com/carabiner-dev/policies
[policyctl]: https://github.com/carabiner-dev/policyctl
[gh]: https://cli.github.com/
