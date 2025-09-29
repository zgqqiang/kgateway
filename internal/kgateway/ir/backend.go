package ir

import (
	"encoding/json"
	"fmt"
	"strings"

	"istio.io/istio/pkg/kube/krt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

type ObjectSource struct {
	Group     string `json:"group,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

// GetKind returns the kind of the route.
func (c ObjectSource) GetGroupKind() schema.GroupKind {
	return schema.GroupKind{
		Group: c.Group,
		Kind:  c.Kind,
	}
}

// GetName returns the name of the route.
func (c ObjectSource) GetName() string {
	return c.Name
}

// GetNamespace returns the namespace of the route.
func (c ObjectSource) GetNamespace() string {
	return c.Namespace
}

func (c ObjectSource) ResourceName() string {
	return fmt.Sprintf("%s/%s/%s/%s", c.Group, c.Kind, c.Namespace, c.Name)
}

func (c ObjectSource) String() string {
	return fmt.Sprintf("%s/%s/%s/%s", c.Group, c.Kind, c.Namespace, c.Name)
}

func (c ObjectSource) Equals(in ObjectSource) bool {
	return c.Namespace == in.Namespace && c.Name == in.Name && c.Group == in.Group && c.Kind == in.Kind
}

type Namespaced interface {
	GetName() string
	GetNamespace() string
}

type AppProtocol string

const (
	DefaultAppProtocol AppProtocol = ""
	HTTP2AppProtocol   AppProtocol = "http2"
)

func ParseAppProtocol(appProtocol *string) AppProtocol {
	switch ptr.Deref(appProtocol, "") {
	case "kubernetes.io/h2c":
		return HTTP2AppProtocol
	default:
		return DefaultAppProtocol
	}
}

type BackendObjectIR struct {
	// Ref to source object. sometimes the group and kind are not populated from api-server, so
	// set them explicitly here, and pass this around as the reference.
	ObjectSource `json:",inline"`
	// optional port for if ObjectSource is a service that can have multiple ports.
	Port int32
	// optional application protocol for the backend. Can be used to enable http2.
	AppProtocol AppProtocol

	// prefix the cluster name with this string to distinguish it from other GVKs.
	// here explicitly as it shows up in stats. each (group, kind) pair should have a unique prefix.
	GvPrefix string
	// for things that integrate with destination rule, we need to know what hostname to use.
	CanonicalHostname string
	// original object. Opaque to us other than metadata.
	Obj metav1.Object

	// can this just be any?
	// i think so, assuming obj -> objir is a 1:1 mapping.
	ObjIr interface{ Equals(any) bool }

	// ExtraKey allows ensuring uniqueness in the KRT key
	// when there is more than one backend per ObjectSource+port.
	// TODO this is a hack for ServiceEntry to workaround only having one
	// CanonicalHostname. We should see if it's possible to have multiple
	// CanonicalHostnames.
	ExtraKey string

	AttachedPolicies AttachedPolicies

	// Errors is a list of errors, if any, encountered while constructing this BackendObject
	// Not added to Equals() as it is derived from the inner ObjIr, which is already evaluated
	Errors []error
}

func (c BackendObjectIR) ResourceName() string {
	return BackendResourceName(c.ObjectSource, c.Port, c.ExtraKey)
}

func BackendResourceName(objSource ObjectSource, port int32, extraKey string) string {
	key := fmt.Sprintf("%s:%d", objSource.ResourceName(), port)
	if extraKey != "" {
		key += extraKey
	}
	return key
}

func (c BackendObjectIR) Equals(in BackendObjectIR) bool {
	objEq := c.ObjectSource.Equals(in.ObjectSource)
	objVersionEq := versionEquals(c.Obj, in.Obj)
	polEq := c.AttachedPolicies.Equals(in.AttachedPolicies)

	// objIr may currently be nil in the case of k8s Services
	// TODO: add an IR for Services to avoid the need for this
	// see: internal/kgateway/extensions2/plugins/kubernetes/k8s.go
	objIrEq := true
	if c.ObjIr != nil {
		objIrEq = c.ObjIr.Equals(in.ObjIr)
	}

	return objEq && objVersionEq && objIrEq && polEq
}

func (c BackendObjectIR) ClusterName() string {
	// TODO: fix this to somthing that's friendly to stats
	gvPrefix := c.GvPrefix
	if c.GvPrefix == "" {
		gvPrefix = strings.ToLower(c.Kind)
	}
	if c.ExtraKey != "" {
		return fmt.Sprintf("%s_%s_%s_%s_%d", gvPrefix, c.Namespace, c.Name, c.ExtraKey, c.Port)
	}
	return fmt.Sprintf("%s_%s_%s_%d", gvPrefix, c.Namespace, c.Name, c.Port)
	// return fmt.Sprintf("%s~%s:%d", c.GvPrefix, c.ObjectSource.ResourceName(), c.Port)
}

func (c BackendObjectIR) GetObjectSource() ObjectSource {
	return c.ObjectSource
}

func (c BackendObjectIR) GetAttachedPolicies() AttachedPolicies {
	return c.AttachedPolicies
}

type Secret struct {
	// Ref to source object. sometimes the group and kind are not populated from api-server, so
	// set them explicitly here, and pass this around as the reference.
	// TODO: why does this have json tag?
	ObjectSource

	// original object. Opaque to us other than metadata.
	Obj metav1.Object

	Data map[string][]byte
}

func (c Secret) ResourceName() string {
	return c.ObjectSource.ResourceName()
}

func (c Secret) Equals(in Secret) bool {
	return c.ObjectSource.Equals(in.ObjectSource) && versionEquals(c.Obj, in.Obj)
}

var (
	_ krt.ResourceNamer   = Secret{}
	_ krt.Equaler[Secret] = Secret{}
	_ json.Marshaler      = Secret{}
)

func (l Secret) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Name      string
		Namespace string
		Kind      string
		Data      string
	}{
		Name:      l.Name,
		Namespace: l.Namespace,
		Kind:      fmt.Sprintf("%T", l.Obj),
		Data:      "[REDACTED]",
	})
}

type Listener struct {
	gwv1.Listener
	AttachedPolicies AttachedPolicies
}

type Gateway struct {
	ObjectSource `json:",inline"`
	Listeners    []Listener
	Obj          *gwv1.Gateway

	AttachedListenerPolicies AttachedPolicies
	AttachedHttpPolicies     AttachedPolicies
}

func (c Gateway) ResourceName() string {
	return c.ObjectSource.ResourceName()
}

func (c Gateway) Equals(in Gateway) bool {
	return c.ObjectSource.Equals(in.ObjectSource) && versionEquals(c.Obj, in.Obj) && c.AttachedListenerPolicies.Equals(in.AttachedListenerPolicies) && c.AttachedHttpPolicies.Equals(in.AttachedHttpPolicies)
}
