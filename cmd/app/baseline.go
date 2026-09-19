// SPDX-License-Identifier: MPL-2.0

package app

import (
	"path/filepath"
)

// embeddedDeployment returns the immutable, digest-scoped deployment for a
// bundled application. Ordinary embedded startup and explicit base recovery
// share these files, but Run gives them different registry history paths.
func embeddedDeployment(state string, bundle Bundle) string {
	return filepath.Join(state, "base", bundleID(bundle))
}

// selectLaunchDeployment applies the host-selected deployment policy. Embedded
// startup resolves the deployment from the executable's own bundle digest and
// leaves the activation record to address the separately updateable deployment.
// The returned history path stays in the state directory, except under explicit
// base recovery, which keeps a history beside the baseline it starts.
func selectLaunchDeployment(state string, bundle Bundle, embedded, base bool, mode string) (string, string, error) {
	if base {
		if mode != "base" {
			return "", "", NewBaseDeploymentUnavailableError(mode)
		}
		// Each executable's embedded content selects an independent baseline,
		// while application databases stay in the state directory.
		deployment := embeddedDeployment(state, bundle)
		return deployment, filepath.Join(deployment, "registry.db"), nil
	}
	history := filepath.Join(state, "registry.db")
	if embedded {
		return embeddedDeployment(state, bundle), history, nil
	}
	deployment, err := selectedDeployment(state)
	if err != nil {
		return "", "", err
	}
	return deployment, history, nil
}
