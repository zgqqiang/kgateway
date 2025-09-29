package wellknown

import (
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	apiv1 "sigs.k8s.io/gateway-api/apis/v1"
	apiv1alpha3 "sigs.k8s.io/gateway-api/apis/v1alpha3"
	apiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

const (
	// Group string for Gateway API resources
	GatewayGroup = apiv1.GroupName

	// Kind strings
	ServiceKind          = "Service"
	HTTPRouteKind        = "HTTPRoute"
	TCPRouteKind         = "TCPRoute"
	TLSRouteKind         = "TLSRoute"
	GatewayKind          = "Gateway"
	GatewayClassKind     = "GatewayClass"
	ReferenceGrantKind   = "ReferenceGrant"
	BackendTLSPolicyKind = "BackendTLSPolicy"

	// Kind string for InferencePool resource
	InferencePoolKind = "InferencePool"

	// List Kind strings
	HTTPRouteListKind      = "HTTPRouteList"
	GatewayListKind        = "GatewayList"
	GatewayClassListKind   = "GatewayClassList"
	ReferenceGrantListKind = "ReferenceGrantList"

	// Gateway API CRD names
	TCPRouteCRDName = "tcproutes.gateway.networking.k8s.io"
)

var (
	GatewayGVK = schema.GroupVersionKind{
		Group:   GatewayGroup,
		Version: apiv1.GroupVersion.Version,
		Kind:    GatewayKind,
	}
	GatewayClassGVK = schema.GroupVersionKind{
		Group:   GatewayGroup,
		Version: apiv1.GroupVersion.Version,
		Kind:    GatewayClassKind,
	}
	HTTPRouteGVK = schema.GroupVersionKind{
		Group:   GatewayGroup,
		Version: apiv1.GroupVersion.Version,
		Kind:    HTTPRouteKind,
	}
	ReferenceGrantGVK = schema.GroupVersionKind{
		Group:   GatewayGroup,
		Version: apiv1beta1.GroupVersion.Version,
		Kind:    ReferenceGrantKind,
	}
	BackendTLSPolicyGVK = schema.GroupVersionKind{
		Group:   GatewayGroup,
		Version: apiv1alpha3.GroupVersion.Version,
		Kind:    BackendTLSPolicyKind,
	}

	TCPRouteCRD = apiextv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: TCPRouteCRDName,
		},
	}
)
