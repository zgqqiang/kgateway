package wellknown

import (
	"k8s.io/apimachinery/pkg/util/sets"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	// GatewayClassName represents the name of the GatewayClass to watch for
	GatewayClassName = "kgateway"

	// WaypointClassName is the GatewayClass name for the waypoint.
	WaypointClassName = "kgateway-waypoint"

	// GatewayControllerName is the name of the controller that has implemented the Gateway API
	// It is configured to manage GatewayClasses with the name GatewayClassName
	GatewayControllerName = "kgateway.dev/kgateway"

	// DefaultGatewayParametersName is the name of the GatewayParameters which is attached by
	// parametersRef to the GatewayClass.
	DefaultGatewayParametersName = "kgateway"

	// InferencePoolFinalizer is the InferencePool finalizer name to ensure cluster-scoped
	// objects are cleaned up.
	InferencePoolFinalizer = "kgateway/inferencepool-cleanup"
)

// BuiltinGatewayClasses are non-extension classe
var BuiltinGatewayClasses = sets.New[gwv1.ObjectName](
	GatewayClassName,
	WaypointClassName,
)
