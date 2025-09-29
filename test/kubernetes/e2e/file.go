package e2e

import (
	"path/filepath"

	"github.com/kgateway-dev/kgateway/v2/pkg/utils/fsutils"
)

var (
	CommonRecommendationManifest = ManifestPath("common-recommendations.yaml")

	// EmptyValuesManifestPath returns the path to a manifest with no values
	// We prefer to have our tests be explicit and require defining a values file. However, some tests
	// rely entirely on the values provided by the "profile". In those cases, the test supplies this reference
	EmptyValuesManifestPath = ManifestPath("empty-values.yaml")

	AIValuesManifestPath = ManifestPath("ai-extension-helm.yaml")
)

// ManifestPath returns the absolute path to a manifest file.
// These are all stored in the tests/manifests directory
func ManifestPath(pathParts ...string) string {
	manifestPathParts := append([]string{
		fsutils.MustGetThisDir(),
		"tests",
		"manifests",
	}, pathParts...)
	return filepath.Join(manifestPathParts...)
}
