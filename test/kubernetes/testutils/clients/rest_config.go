//go:build ignore

package clients

import (
	"k8s.io/client-go/rest"

	"github.com/kgateway-dev/kgateway/v2/pkg/utils/kubeutils"
)

// MustRestConfig returns MustRestConfigWithContext with an empty Kubernetes Context
func MustRestConfig() *rest.Config {
	restConfig, err := kubeutils.GetRestConfigWithKubeContext("")
	if err != nil {
		panic(err)
	}

	return restConfig
}
