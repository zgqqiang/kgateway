package settings

import (
	"github.com/kelseyhightower/envconfig"
)

type Settings struct {
	// Controls the DnsLookupFamily for all static clusters created via Backend resources.
	// If not set, kgateway will default to "V4_PREFERRED". Note that this is different
	// from the Envoy default of "AUTO", which is effectively "V6_PREFERRED".
	// Supported values are: "ALL", "AUTO", "V4_PREFERRED", "V4_ONLY", "V6_ONLY"
	// Details on the behavior of these options are available on the Envoy documentation:
	// https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/cluster/v3/cluster.proto#enum-config-cluster-v3-cluster-dnslookupfamily
	DnsLookupFamily string `split_words:"true" default:"V4_PREFERRED"`

	EnableIstioIntegration bool `split_words:"true"`
	EnableIstioAutoMtls    bool `split_words:"true"`

	// IstioNamespace is the namespace where Istio control plane components are installed.
	// Defaults to "istio-system".
	IstioNamespace string `split_words:"true" default:"istio-system"`

	// XdsServiceName is the name of the Kubernetes Service that serves xDS config.
	// It it assumed to be in the kgateway install namespace.
	XdsServiceName string `split_words:"true" default:"kgateway"`

	// XdsServicePort is the port of the Kubernetes Service that serves xDS config.
	// This corresponds to the value of the `grpc-xds` port in the service.
	XdsServicePort uint32 `split_words:"true" default:"9977"`

	UseRustFormations bool `split_words:"true" default:"false"`

	// EnableInferExt defines whether to enable/disable support for Gateway API inference extension.
	EnableInferExt bool `split_words:"true"`
	// InferExtAutoProvision defines whether to enable/disable the Gateway API inference extension deployer.
	InferExtAutoProvision bool `split_words:"true"`

	// DefaultImageRegistry is the default image registry to use for the kgateway image.
	DefaultImageRegistry string `split_words:"true" default:"cr.kgateway.dev"`
	// DefaultImageTag is the default image tag to use for the kgateway image.
	DefaultImageTag string `split_words:"true" default:""`
	// DefaultImagePullPolicy is the default image pull policy to use for the kgateway image.
	DefaultImagePullPolicy string `split_words:"true" default:"IfNotPresent"`
}

// BuildSettings returns a zero-valued Settings obj if error is encountered when parsing env
func BuildSettings() (*Settings, error) {
	settings := &Settings{}
	if err := envconfig.Process("KGW", settings); err != nil {
		return settings, err
	}
	return settings, nil
}
