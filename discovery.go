// SPDX-FileCopyrightText: Copyright 2026 Carabiner Systems, Inc
// SPDX-License-Identifier: Apache-2.0

package lexecutor

import (
	"io/fs"
	"path/filepath"

	"github.com/carabiner-dev/policyctl/pkg/tester"
)

// DiscoveredSuite is a test suite found during discovery.
type DiscoveredSuite struct {
	Dir   string            // directory containing the config file
	Suite *tester.TestSuite // parsed config
}

// Discover walks rootDir looking for .ptests.yaml files and returns
// the parsed suites.
func Discover(rootDir string) ([]DiscoveredSuite, error) {
	var suites []DiscoveredSuite

	err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() != ".ptests.yaml" {
			return nil
		}

		suite, err := tester.LoadConfig(path)
		if err != nil {
			return err
		}

		suites = append(suites, DiscoveredSuite{
			Dir:   filepath.Dir(path),
			Suite: suite,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	return suites, nil
}
