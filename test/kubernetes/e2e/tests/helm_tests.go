//go:build ignore

package tests

import (
	"github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e"
	"github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e/features/helm"
	"github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e/features/helm_settings"
)

func HelmSuiteRunner() e2e.SuiteRunner {
	helmSuiteRunner := e2e.NewSuiteRunner(false)
	helmSuiteRunner.Register("Helm", helm.NewTestingSuite)
	return helmSuiteRunner
}

func HelmSettingsSuiteRunner() e2e.SuiteRunner {
	runner := e2e.NewSuiteRunner(false)
	runner.Register("HelmSettings", helm_settings.NewTestingSuite)
	return runner
}
