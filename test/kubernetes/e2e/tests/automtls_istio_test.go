//go:build ignore

package tests_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/kgateway-dev/kgateway/v2/pkg/utils/envutils"
	"github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e"
	. "github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e/tests"
	"github.com/kgateway-dev/kgateway/v2/test/kubernetes/testutils/install"
	"github.com/kgateway-dev/kgateway/v2/test/testutils"
)

// TestKgatewayIstioAutoMtls is the function which executes a series of tests against a given installation
func TestKgatewayIstioAutoMtls(t *testing.T) {
	ctx := context.Background()
	installNs, nsEnvPredefined := envutils.LookupOrDefault(testutils.InstallNamespace, "automtls-istio-test")
	testInstallation := e2e.CreateTestInstallation(
		t,
		&install.Context{
			InstallNamespace:          installNs,
			ProfileValuesManifestFile: e2e.CommonRecommendationManifest,
			ValuesManifestFile:        e2e.ManifestPath("istio-automtls-enabled-helm.yaml"),
		},
	)

	testHelper := e2e.MustTestHelper(ctx, testInstallation)
	err := testInstallation.AddIstioctl(ctx)
	if err != nil {
		t.Errorf("failed to add istioctl: %v\n", err)
	}

	// Set the env to the install namespace if it is not already set
	if !nsEnvPredefined {
		os.Setenv(testutils.InstallNamespace, installNs)
	}

	// We register the cleanup function _before_ we actually perform the installation.
	// This allows us to uninstall Gloo Gateway, in case the original installation only completed partially
	t.Cleanup(func() {
		if !nsEnvPredefined {
			os.Unsetenv(testutils.InstallNamespace)
		}
		if t.Failed() {
			testInstallation.PreFailHandler(ctx)

			// Generate istioctl bug report
			testInstallation.CreateIstioBugReport(ctx)
		}

		testInstallation.UninstallGlooGatewayWithTestHelper(ctx, testHelper)

		// Uninstall Istio
		err = testInstallation.UninstallIstio()
		if err != nil {
			t.Errorf("failed to add istioctl: %v\n", err)
		}
	})

	// Install Istio before Gloo Gateway to make sure istiod is present before istio-proxy
	err = testInstallation.InstallMinimalIstio(ctx)
	if err != nil {
		t.Errorf("failed to add istioctl: %v\n", err)
	}

	// Install Gloo Gateway
	// istio proxy and sds are added to gateway and take a little longer to start up
	testInstallation.InstallGlooGatewayWithTestHelper(ctx, testHelper, 10*time.Minute)

	AutomtlsIstioSuiteRunner().Run(ctx, t, testInstallation)
}
