package ir

import (
	"reflect"

	"istio.io/istio/pkg/kube/krt"

	"github.com/kgateway-dev/kgateway/v2/api/v1alpha1"
)

// GatewayExtension represents the internal representation of a GatewayExtension.
type GatewayExtension struct {
	// ObjectSource identifies the source of this extension.
	ObjectSource

	// Type indicates the type of the GatewayPolicy.
	Type v1alpha1.GatewayExtensionType

	// ExtAuth configuration for ExtAuth extension type.
	ExtAuth *v1alpha1.ExtAuthProvider

	// ExtProc configuration for ExtProc extension type.
	ExtProc *v1alpha1.ExtProcProvider
}

var (
	_ krt.ResourceNamer             = GatewayExtension{}
	_ krt.Equaler[GatewayExtension] = GatewayExtension{}
)

// ResourceName returns the unique name for this extension.
func (e GatewayExtension) ResourceName() string {
	return e.ObjectSource.ResourceName()
}

func (e GatewayExtension) Equals(other GatewayExtension) bool {
	if e.Type != other.Type {
		return false
	}
	if !reflect.DeepEqual(e.ExtAuth, other.ExtAuth) {
		return false
	}
	return true
}
