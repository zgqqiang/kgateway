package wellknown

const (
	// Note: These are coming from istio: https://github.com/istio/istio/blob/fa321ebd2a1186325788b0f461aa9f36a1a8d90e/pilot/pkg/model/service.go#L206
	// IstioCertSecret is the secret that holds the server cert and key for Istio mTLS
	IstioCertSecret = "istio_server_cert"

	// IstioValidationContext is the secret that holds the root cert for Istio mTLS
	IstioValidationContext = "istio_validation_context"

	// IstioTlsModeLabel is the Istio injection label added to workloads in mesh
	IstioTlsModeLabel = "security.istio.io/tlsMode"

	// IstioMutualTLSModeLabel implies that the endpoint is ready to receive Istio mTLS connections.
	IstioMutualTLSModeLabel = "istio"

	// TLSModeLabelShortname name used for determining endpoint level tls transport socket configuration
	TLSModeLabelShortname = "tlsMode"
)

const (
	SdsClusterName = "gateway_proxy_sds"
	SdsTargetURI   = "127.0.0.1:8234"
)

const (
	InfPoolTransformationFilterName   = "inferencepool.backend.transformation.kgateway.io"
	AIBackendTransformationFilterName = "ai.backend.transformation.kgateway.io"
	AIPolicyTransformationFilterName  = "ai.policy.transformation.kgateway.io"
	AIExtProcFilterName               = "ai.extproc.kgateway.io"
	SetMetadataFilterName             = "envoy.filters.http.set_filter_state"
	ExtprocFilterName                 = "envoy.filters.http.ext_proc"
)

const (
	EnvoyConfigNameMaxLen = 253
)
