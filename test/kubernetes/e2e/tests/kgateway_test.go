package tests_test

import (
	"context"
	"os"
	"testing"

	"github.com/kgateway-dev/kgateway/v2/pkg/utils/envutils"
	"github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e"
	. "github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e/tests"
	"github.com/kgateway-dev/kgateway/v2/test/kubernetes/testutils/install"
	"github.com/kgateway-dev/kgateway/v2/test/testutils"
)

// TestKgateway is the function which executes a series of tests against a given installation
func TestKgateway(t *testing.T) {
	ctx := context.Background()
	installNs, nsEnvPredefined := envutils.LookupOrDefault(testutils.InstallNamespace, "kgateway-test")
	testInstallation := e2e.CreateTestInstallation(
		t,
		&install.Context{
			InstallNamespace:          installNs,
			ProfileValuesManifestFile: e2e.CommonRecommendationManifest,
			ValuesManifestFile:        e2e.EmptyValuesManifestPath,
		},
	)

	// Set the env to the install namespace if it is not already set
	if !nsEnvPredefined {
		os.Setenv(testutils.InstallNamespace, installNs)
	}

	// We register the cleanup function _before_ we actually perform the installation.
	// This allows us to uninstall kgateway, in case the original installation only completed partially
	t.Cleanup(func() {
		if !nsEnvPredefined {
			os.Unsetenv(testutils.InstallNamespace)
		}
		if t.Failed() {
			testInstallation.PreFailHandler(ctx)
		}

		testInstallation.UninstallKgateway(ctx)
	})

	// Install kgateway
	testInstallation.InstallKgatewayFromLocalChart(ctx)

	KubeGatewaySuiteRunner().Run(ctx, t, testInstallation)
}
