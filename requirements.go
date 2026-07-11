// SPDX-FileCopyrightText: Copyright 2026 Carabiner Systems, Inc
// SPDX-License-Identifier: Apache-2.0

package lexecutor

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/carabiner-dev/policy"
	papi "github.com/carabiner-dev/policy/api/v1"
)

// policyRequiredPlugins compiles the policy at policyPath and returns the union
// of plugin requirements declared in the runtime specifiers of every policy it
// contains (the policy-level runtime plus each tenet runtime). The result is
// keyed by plugin name to the required version. An empty map means the policy
// runs on a bare engine and needs no plugins, so it can execute on any runner.
func policyRequiredPlugins(policyPath string) (map[string]string, error) {
	set, pcy, grp, err := policy.NewCompiler().CompileFile(policyPath)
	if err != nil {
		return nil, fmt.Errorf("compiling policy %s: %w", policyPath, err)
	}

	required := map[string]string{}
	for _, p := range collectPolicies(set, pcy, grp) {
		for _, runtime := range policyRuntimes(p) {
			for name, version := range requiredPluginsFromRuntime(runtime) {
				required[name] = version
			}
		}
	}
	return required, nil
}

// policyRuntimes returns every runtime specifier declared by a policy: the
// policy-level default plus each tenet's own runtime.
func policyRuntimes(p *papi.Policy) []string {
	runtimes := []string{p.GetMeta().GetRuntime()}
	for _, t := range p.GetTenets() {
		runtimes = append(runtimes, t.GetRuntime())
	}
	return runtimes
}

// requiredPluginsFromRuntime parses a runtime class specifier of the form
// "name@version?plugin:x=v1&transformer:y=v1" and returns its plugin
// name->version requirements. This mirrors the plugin portion of ampel's
// evaluator class format; transformer and base-engine parts are ignored.
// Returns nil when the specifier declares no plugin requirements.
func requiredPluginsFromRuntime(runtime string) map[string]string {
	_, qs, ok := strings.Cut(runtime, "?")
	if !ok {
		return nil
	}
	vals, err := url.ParseQuery(qs)
	if err != nil {
		return nil
	}
	var out map[string]string
	for key, versions := range vals {
		name, found := strings.CutPrefix(key, "plugin:")
		if !found || name == "" {
			continue
		}
		version := ""
		if len(versions) > 0 {
			version = versions[0]
		}
		if out == nil {
			out = map[string]string{}
		}
		out[name] = version
	}
	return out
}

// collectPolicies flattens whichever container the compiler produced (a single
// policy, a group, or a set of policies and groups) into one slice.
func collectPolicies(set *papi.PolicySet, pcy *papi.Policy, grp *papi.PolicyGroup) []*papi.Policy {
	switch {
	case pcy != nil:
		return []*papi.Policy{pcy}
	case grp != nil:
		return policiesFromGroup(grp)
	case set != nil:
		policies := append([]*papi.Policy{}, set.GetPolicies()...)
		for _, g := range set.GetGroups() {
			policies = append(policies, policiesFromGroup(g)...)
		}
		return policies
	default:
		return nil
	}
}

func policiesFromGroup(grp *papi.PolicyGroup) []*papi.Policy {
	var policies []*papi.Policy
	for _, b := range grp.GetBlocks() {
		policies = append(policies, b.GetPolicies()...)
	}
	return policies
}
