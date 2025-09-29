package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:rbac:groups=gateway.kgateway.dev,resources=trafficpolicies,verbs=get;list;watch
// +kubebuilder:rbac:groups=gateway.kgateway.dev,resources=trafficpolicies/status,verbs=get;update;patch

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:metadata:labels={app=kgateway,app.kubernetes.io/name=kgateway}
// +kubebuilder:resource:categories=kgateway
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="gateway.networking.k8s.io/policy=Direct"
type TrafficPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TrafficPolicySpec `json:"spec,omitempty"`
	Status SimpleStatus      `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type TrafficPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TrafficPolicy `json:"items"`
}

type TrafficPolicySpec struct {
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	TargetRefs []LocalPolicyTargetReference `json:"targetRefs,omitempty"`

	// AI is used to configure AI-based policies for the policy.
	// +optional
	AI *AIPolicy `json:"ai,omitempty"`

	// Transformation is used to mutate and transform requests and responses
	// before forwarding them to the destination.
	// +optional
	Transformation *TransformationPolicy `json:"transformation,omitempty"`

	// ExtProc specifies the external processing configuration for the policy.
	// +optional
	ExtProc *ExtProcPolicy `json:"extProc,omitempty"`

	// ExtAuth specifies the external authentication configuration for the policy.
	// This controls what external server to send requests to for authentication.
	// +optional
	ExtAuth *ExtAuthPolicy `json:"extAuth,omitempty"`

	// RateLimit specifies the rate limiting configuration for the policy.
	// This controls the rate at which requests are allowed to be processed.
	// +optional
	RateLimit *RateLimit `json:"rateLimit,omitempty"`
}

// TransformationPolicy config is used to modify envoy behavior at a route level.
// These modifications can be performed on the request and response paths.
type TransformationPolicy struct {
	// Request is used to modify the request path.
	// +optional
	Request *Transform `json:"request,omitempty"`

	// Response is used to modify the response path.
	// +optional
	Response *Transform `json:"response,omitempty"`
}

// Transform defines the operations to be performed by the transformation.
// These operations may include changing the actual request/response but may also cause side effects.
// Side effects may include setting info that can be used in future steps (e.g. dynamic metadata) and can cause envoy to buffer.
type Transform struct {
	// Set is a list of headers and the value they should be set to.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=16
	Set []HeaderTransformation `json:"set,omitempty"`

	// Add is a list of headers to add to the request and what that value should be set to.
	// If there is already a header with these values then append the value as an extra entry.
	// +optional
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=16
	Add []HeaderTransformation `json:"add,omitempty"`

	// Remove is a list of header names to remove from the request/response.
	// +optional
	// +listType=set
	// +kubebuilder:validation:MaxItems=16
	Remove []string `json:"remove,omitempty"`

	// Body controls both how to parse the body and if needed how to set.
	// If empty, body will not be buffered.
	// +optional
	Body *BodyTransformation `json:"body,omitempty"`
}

type InjaTemplate string

// EnvoyHeaderName is the name of a header or pseudo header
// Based on gateway api v1.Headername but allows a singular : at the start
//
// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=256
// +kubebuilder:validation:Pattern=`^:?[A-Za-z0-9!#$%&'*+\-.^_\x60|~]+$`
// +k8s:deepcopy-gen=false
type (
	HeaderName           string
	HeaderTransformation struct {
		// Name is the name of the header to interact with.
		// +required
		Name HeaderName `json:"name,omitempty"`
		// Value is the template to apply to generate the output value for the header.
		Value InjaTemplate `json:"value,omitempty"`
	}
)

// BodyparseBehavior defines how the body should be parsed
// If set to json and the body is not json then the filter will not perform the transformation.
// +kubebuilder:validation:Enum=AsString;AsJson
type BodyParseBehavior string

const (
	// BodyParseBehaviorAsString will parse the body as a string.
	BodyParseBehaviorAsString BodyParseBehavior = "AsString"
	// BodyParseBehaviorAsJSON will parse the body as a json object.
	BodyParseBehaviorAsJSON BodyParseBehavior = "AsJson"
)

// BodyTransformation controls how the body should be parsed and transformed.
type BodyTransformation struct {
	// ParseAs defines what auto formatting should be applied to the body.
	// This can make interacting with keys within a json body much easier if AsJson is selected.
	// +kubebuilder:default=AsString
	ParseAs BodyParseBehavior `json:"parseAs"`

	// Value is the template to apply to generate the output value for the body.
	// +optional
	Value *InjaTemplate `json:"value,omitempty"`
}

// SimpleStatus defines the observed state of the policy.
type SimpleStatus struct {
	// Conditions is the list of conditions for the policy.
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=8
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ExtAuthEnabled determines the enabled state of the ExtAuth filter.
// +kubebuilder:validation:Enum=DisableAll
type ExtAuthEnabled string

// When we add a new field here we have to be specific around which extensions are enabled/disabled
// and how these can be overridden by other policies.
const (
	// ExtAuthDisableAll disables all instances of the ExtAuth filter for this route.
	// This is to enable a global disable such as for a health check route.
	ExtAuthDisableAll ExtAuthEnabled = "DisableAll"
)

// ExtAuthPolicy configures external authentication for a route.
// This policy will determine the ext auth server to use and how to  talk to it.
// Note that most of these fields are passed along as is to Envoy.
// For more details on particular fields please see the Envoy ExtAuth documentation.
// https://raw.githubusercontent.com/envoyproxy/envoy/f910f4abea24904aff04ec33a00147184ea7cffa/api/envoy/extensions/filters/http/ext_authz/v3/ext_authz.proto
// +kubebuilder:validation:XValidation:message="only one of 'extensionRef' or 'enablement' may be set",rule="(has(self.extensionRef) && !has(self.enablement)) || (!has(self.extensionRef) && has(self.enablement))"
type ExtAuthPolicy struct {
	// ExtensionRef references the ExternalExtension that should be used for authentication.
	// +optional
	ExtensionRef *corev1.LocalObjectReference `json:"extensionRef,omitempty"`

	// Enablement determines the enabled state of the ExtAuth filter.
	// When set to "DisableAll", the filter is disabled for this route.
	// When empty, the filter is enabled as long as it is not disabled by another policy.
	// +optional
	Enablement ExtAuthEnabled `json:"enablement,omitempty"`

	// FailureModeAllow determines the behavior on authorization service errors.
	// When true, requests will be allowed even if the authorization service fails or returns HTTP 5xx errors.
	// When unset, the default behavior is false.
	// +optional
	FailureModeAllow *bool `json:"failureModeAllow,omitempty"`

	// WithRequestBody allows the request body to be buffered and sent to the authorization service.
	// Warning buffering has implications for streaming and therefore performance.
	// +optional
	WithRequestBody *BufferSettings `json:"withRequestBody,omitempty"`

	// ClearRouteCache allows the authorization service to affect routing decisions.
	// When unset, the default behavior is false.
	// +optional
	ClearRouteCache *bool `json:"clearRouteCache,omitempty"`

	// MetadataContextNamespaces specifies metadata namespaces to pass to the authorization service.
	// Default to allowing jwt info if processing for jwt is configured.
	// +optional
	// +listType=set
	// +kubebuilder:default={"jwt"}
	MetadataContextNamespaces []string `json:"metadataContextNamespaces,omitempty"`

	// IncludePeerCertificate determines if the client's X.509 certificate should be sent to the authorization service.
	// When true, the certificate will be included if available.
	// When unset, the default behavior is false.
	// +optional
	IncludePeerCertificate *bool `json:"includePeerCertificate,omitempty"`

	// IncludeTLSSession determines if TLS session details should be sent to the authorization service.
	// When true, the SNI name from TLSClientHello will be included if available.
	// When unset, the default behavior is false.
	// +optional
	IncludeTLSSession *bool `json:"includeTLSSession,omitempty"`

	// EmitFilterStateStats determines if per-stream stats should be emitted for access logging.
	// When true and using Envoy gRPC, emits latency, bytes sent/received, and upstream info.
	// When true and not using Envoy gRPC, emits only latency.
	// Stats are only added if a check request is made to the ext_authz service.
	// When unset, the default behavior is false.
	// +optional
	EmitFilterStateStats *bool `json:"emitFilterStateStats,omitempty"`
}

// BufferSettings configures how the request body should be buffered.
type BufferSettings struct {
	// MaxRequestBytes sets the maximum size of a message body to buffer.
	// Requests exceeding this size will receive HTTP 413 and not be sent to the authorization service.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	MaxRequestBytes uint32 `json:"maxRequestBytes"`

	// AllowPartialMessage determines if partial messages should be allowed.
	// When true, requests will be sent to the authorization service even if they exceed maxRequestBytes.
	// When unset, the default behavior is false.
	// +optional
	AllowPartialMessage *bool `json:"allowPartialMessage,omitempty"`

	// PackAsBytes determines if the body should be sent as raw bytes.
	// When true, the body is sent as raw bytes in the raw_body field.
	// When false, the body is sent as UTF-8 string in the body field.
	// When unset, the default behavior is false.
	// +optional
	PackAsBytes *bool `json:"packAsBytes,omitempty"`
}

// RateLimit defines a rate limiting policy.
type RateLimit struct {
	// Local defines a local rate limiting policy.
	// +required
	Local *LocalRateLimitPolicy `json:"local"`
}

// LocalRateLimitPolicy represents a policy for local rate limiting.
// It defines the configuration for rate limiting using a token bucket mechanism.
type LocalRateLimitPolicy struct {
	// TokenBucket represents the configuration for a token bucket local rate-limiting mechanism.
	// It defines the parameters for controlling the rate at which requests are allowed.
	// +optional
	TokenBucket *TokenBucket `json:"tokenBucket"`
}

// TokenBucket defines the configuration for a token bucket rate-limiting mechanism.
// It controls the rate at which tokens are generated and consumed for a specific operation.
type TokenBucket struct {
	// MaxTokens specifies the maximum number of tokens that the bucket can hold.
	// This value must be greater than or equal to 1.
	// It determines the burst capacity of the rate limiter.
	// +required
	// +kubebuilder:validation:Minimum=1
	MaxTokens uint32 `json:"maxTokens"`

	// TokensPerFill specifies the number of tokens added to the bucket during each fill interval.
	// If not specified, it defaults to 1.
	// This controls the steady-state rate of token generation.
	// +optional
	// +kubebuilder:default:=1
	TokensPerFill *uint32 `json:"tokensPerFill,omitempty"`

	// FillInterval defines the time duration between consecutive token fills.
	// This value must be a valid duration string (e.g., "1s", "500ms").
	// It determines the frequency of token replenishment.
	// +required
	// +kubebuilder:validation:Format=duration
	FillInterval string `json:"fillInterval"`
}
