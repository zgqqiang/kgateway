package query

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"istio.io/istio/pkg/kube/krt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwv1a2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/ir"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/wellknown"
)

// TODO (danehans): Rename this file to route.go since it supports different route types.

// RouteInfo contains pre-resolved backends (Services, Upstreams and delegated xRoutes)
// This allows all querying to happen upfront, and detailed logic for delegation to happen
// as part of translation.
type RouteInfo struct {
	// Object is the generic route object which could be HTTPRoute, TCPRoute, etc.
	Object ir.Route

	// ParentRef points to the Gateway (and optionally Listener) or HTTPRoute.
	ParentRef gwv1.ParentReference

	// ParentRef points to the Gateway (and optionally Listener).
	ListenerParentRef gwv1.ParentReference

	// hostnameOverrides can replace the HTTPRoute hostnames with those that intersect
	// the attached listener's hostname(s).
	HostnameOverrides []string

	// Children contains all delegate HTTPRoutes referenced in any rule of this
	// HTTPRoute, keyed by the backend ref for easy lookup.
	// This tree structure can have cyclic references. Check them when recursing through the tree.
	Children BackendMap[[]*RouteInfo]
}

// GetKind returns the kind of the route.
func (r RouteInfo) GetKind() string {
	return r.Object.GetGroupKind().Kind
}

// GetName returns the name of the route.
func (r RouteInfo) GetName() string {
	return r.Object.GetName()
}

// GetNamespace returns the namespace of the route.
func (r RouteInfo) GetNamespace() string {
	return r.Object.GetNamespace()
}

// Hostnames returns the hostname overrides if they exist, otherwise it returns
// the hostnames specified in the HTTPRoute.
func (r *RouteInfo) Hostnames() []string {
	if len(r.HostnameOverrides) > 0 {
		return r.HostnameOverrides
	}

	httpRoute, ok := r.Object.(*ir.HttpRouteIR)
	if !ok {
		return []string{}
	}

	if httpRoute.Hostnames != nil {
		return httpRoute.Hostnames
	}
	return []string{}
}

// GetChildrenForRef fetches child routes for a given BackendObjectReference.
func (r *RouteInfo) GetChildrenForRef(backendRef ir.ObjectSource) ([]*RouteInfo, error) {
	return r.Children.get(backendRef, nil)
}

// Clone creates a deep copy of the RouteInfo object.
func (r *RouteInfo) Clone() *RouteInfo {
	if r == nil {
		return nil
	}
	// TODO (danehans): Why are hostnameOverrides not being cloned?
	return &RouteInfo{
		Object:            r.Object,
		ParentRef:         r.ParentRef,
		ListenerParentRef: r.ListenerParentRef,
		Children:          r.Children,
	}
}

// UniqueRouteName returns a unique name for the route based on the route kind, name, namespace,
// and the given indexes.
func (r *RouteInfo) UniqueRouteName(ruleIdx, matchIdx int) string {
	return fmt.Sprintf("%s-%s-%s-%d-%d", strings.ToLower(r.GetKind()), r.GetName(), r.GetNamespace(), ruleIdx, matchIdx)
}

// GetRouteChain recursively resolves all backends for the given route object.
// It handles delegation of HTTPRoutes and resolves child routes.
func (r *gatewayQueries) GetRouteChain(
	kctx krt.HandlerContext,
	ctx context.Context,
	route ir.Route,
	hostnames []string,
	parentRef gwv1.ParentReference,
) *RouteInfo {
	var children BackendMap[[]*RouteInfo]

	switch typedRoute := route.(type) {
	case *ir.HttpRouteIR:
		children = r.getDelegatedChildren(kctx, ctx, parentRef, typedRoute, nil)
	case *ir.TcpRouteIR:
		// TODO (danehans): Should TCPRoute delegation support be added in the future?
	case *ir.TlsRouteIR:
	default:
		return nil
	}

	return &RouteInfo{
		Object:            route,
		HostnameOverrides: hostnames,
		ParentRef:         parentRef,
		ListenerParentRef: parentRef,
		Children:          children,
	}
}

func (r *gatewayQueries) allowedRoutes(gw *gwv1.Gateway, l *gwv1.Listener) (func(krt.HandlerContext, string) bool, []metav1.GroupKind, error) {
	var allowedKinds []metav1.GroupKind

	// Determine the allowed route kinds based on the listener's protocol
	switch l.Protocol {
	case gwv1.HTTPSProtocolType:
		fallthrough
	case gwv1.HTTPProtocolType:
		allowedKinds = []metav1.GroupKind{{Kind: wellknown.HTTPRouteKind, Group: gwv1.GroupName}}
	case gwv1.TLSProtocolType:
		allowedKinds = []metav1.GroupKind{{Kind: wellknown.TLSRouteKind, Group: gwv1.GroupName}}
	case gwv1.TCPProtocolType:
		allowedKinds = []metav1.GroupKind{{Kind: wellknown.TCPRouteKind, Group: gwv1a2.GroupName}}
	case gwv1.UDPProtocolType:
		allowedKinds = []metav1.GroupKind{{}}
	default:
		// allow custom protocols to work
		allowedKinds = []metav1.GroupKind{{Kind: wellknown.HTTPRouteKind, Group: gwv1.GroupName}}
	}

	allowedNs := SameNamespace(gw.Namespace)
	if ar := l.AllowedRoutes; ar != nil {
		// Override the allowed route kinds if specified in AllowedRoutes
		if ar.Kinds != nil {
			allowedKinds = nil // Reset to include only explicitly allowed kinds
			for _, k := range ar.Kinds {
				gk := metav1.GroupKind{Kind: string(k.Kind)}
				if k.Group != nil {
					gk.Group = string(*k.Group)
				} else {
					gk.Group = gwv1.GroupName
				}
				allowedKinds = append(allowedKinds, gk)
			}
		}

		// Determine the allowed namespaces if specified
		if ar.Namespaces != nil && ar.Namespaces.From != nil {
			switch *ar.Namespaces.From {
			case gwv1.NamespacesFromAll:
				allowedNs = AllNamespace()
			case gwv1.NamespacesFromSelector:
				if ar.Namespaces.Selector == nil {
					return nil, nil, fmt.Errorf("selector must be set")
				}
				selector, err := metav1.LabelSelectorAsSelector(ar.Namespaces.Selector)
				if err != nil {
					return nil, nil, err
				}
				allowedNs = r.NamespaceSelector(selector)
			}
		}
	}

	return allowedNs, allowedKinds, nil
}

func (r *gatewayQueries) getDelegatedChildren(
	kctx krt.HandlerContext,
	ctx context.Context,
	listenerRef gwv1.ParentReference,
	parent *ir.HttpRouteIR,
	visited sets.Set[types.NamespacedName],
) BackendMap[[]*RouteInfo] {
	// Initialize the set of visited routes if it hasn't been initialized yet
	if visited == nil {
		visited = sets.New[types.NamespacedName]()
	}
	parentRef := namespacedName(parent)
	// `visited` is used to detect cyclic references to routes in the delegation chain.
	// It is important to remove the route from the set once all its children have been evaluated
	// in the recursion stack, because a route may have multiple parents that have the same ancestor:
	// e.g., A -> B1, A -> B2, B1 -> C, B2 -> C. So in this case, even though C is visited twice,
	// the delegation chain is valid as it is evaluated only once for each parent.
	visited.Insert(parentRef)
	defer visited.Delete(parentRef)

	children := NewBackendMap[[]*RouteInfo]()
	for _, parentRule := range parent.Rules {
		var refChildren []*RouteInfo
		for _, backendRef := range parentRule.Backends {
			// Check if the backend reference is an HTTPRoute
			if backendRef.Delegate == nil {
				continue
			}
			ref := *backendRef.Delegate
			// Fetch child routes based on the backend reference
			referencedRoutes, err := r.fetchChildRoutes(kctx, backendRef)
			if err != nil {
				children.AddError(ref, err)
				continue
			}
			for _, childRoute := range referencedRoutes {
				childRef := namespacedName(&childRoute)
				if visited.Has(childRef) {
					err := fmt.Errorf("ignoring child route %s for parent %s: %w", childRef, parentRef, ErrCyclicReference)
					children.AddError(ref, err)
					// Don't resolve invalid child route
					continue
				}
				// Recursively get the route chain for each child route
				routeInfo := &RouteInfo{
					Object: &childRoute,
					ParentRef: gwv1.ParentReference{
						Group:     ptr.To(gwv1.Group(wellknown.GatewayGroup)),
						Kind:      ptr.To(gwv1.Kind(wellknown.HTTPRouteKind)),
						Namespace: ptr.To(gwv1.Namespace(parent.Namespace)),
						Name:      gwv1.ObjectName(parent.Name),
					},
					ListenerParentRef: listenerRef,
					Children:          r.getDelegatedChildren(kctx, ctx, listenerRef, &childRoute, visited),
				}
				refChildren = append(refChildren, routeInfo)
			}
			// Add the resolved children routes to the backend map
			children.Add(ref, refChildren)
		}
	}
	return children
}

func (r *gatewayQueries) fetchChildRoutes(
	kctx krt.HandlerContext,
	backend ir.HttpBackendOrDelegate,
) ([]ir.HttpRouteIR, error) {
	if backend.Delegate == nil {
		return nil, nil
	}
	backendRef := *backend.Delegate
	delegatedNs := backendRef.Namespace

	var refChildren []ir.HttpRouteIR
	if string(backendRef.Name) == "" || string(backendRef.Name) == "*" {
		// Handle wildcard references by listing all HTTPRoutes in the specified namespace
		routes := r.collections.Routes.FetchHttpNamespace(kctx, delegatedNs)
		refChildren = append(refChildren, routes...)
	} else {
		// Lookup a specific child route by its name
		route := r.collections.Routes.FetchHttp(kctx, delegatedNs, string(backendRef.Name))
		if route == nil {
			return nil, errors.New("not found")
		}
		refChildren = append(refChildren, *route)
	}
	// Check if no child routes were resolved and log an error if needed
	if len(refChildren) == 0 {
		return nil, ErrUnresolvedReference
	}

	return refChildren, nil
}

func (r *gatewayQueries) GetRoutesForGateway(kctx krt.HandlerContext, ctx context.Context, gw *gwv1.Gateway) (*RoutesForGwResult, error) {
	nns := types.NamespacedName{
		Namespace: gw.Namespace,
		Name:      gw.Name,
	}

	// Process each route
	ret := NewRoutesForGwResult()
	routes := r.collections.Routes.RoutesForGateway(kctx, nns)
	for _, route := range routes {
		if err := r.processRoute(kctx, ctx, gw, route, ret); err != nil {
			return nil, err
		}
	}

	return ret, nil
}

func (r *gatewayQueries) processRoute(
	kctx krt.HandlerContext,
	ctx context.Context,
	gw *gwv1.Gateway,
	route ir.Route,
	ret *RoutesForGwResult,
) error {
	refs := getParentRefsForGw(gw, route)
	routeKind := route.GetGroupKind().Kind

	for _, ref := range refs {
		anyRoutesAllowed := false
		anyListenerMatched := false
		anyHostsMatch := false

		for _, l := range gw.Spec.Listeners {
			lr := ret.ListenerResults[string(l.Name)]
			if lr == nil {
				lr = &ListenerResult{}
				ret.ListenerResults[string(l.Name)] = lr
			}

			allowedNs, allowedKinds, err := r.allowedRoutes(gw, &l)
			if err != nil {
				lr.Error = err
				continue
			}

			// Check if the kind of the route is allowed by the listener
			if !isKindAllowed(routeKind, allowedKinds) {
				continue
			}

			// Check if the namespace of the route is allowed by the listener
			if !allowedNs(kctx, route.GetNamespace()) {
				continue
			}
			anyRoutesAllowed = true

			// Check if the listener matches the route's parent reference
			if !parentRefMatchListener(&ref, &l) {
				continue
			}
			anyListenerMatched = true

			// If the route is an HTTP or TLS Route, check the hostname intersection
			var hostnames []string
			if routeKind == wellknown.HTTPRouteKind {
				if hr, ok := route.(*ir.HttpRouteIR); ok {
					var ok bool
					ok, hostnames = hostnameIntersect(&l, hr.GetHostnames())
					if !ok {
						continue
					}
					anyHostsMatch = true
				}
			}
			if routeKind == wellknown.TLSRouteKind {
				if tr, ok := route.(*ir.TlsRouteIR); ok {
					var ok bool
					ok, hostnames = hostnameIntersect(&l, tr.GetHostnames())
					if !ok {
						continue
					}
					anyHostsMatch = true
				}
			}

			// If all checks pass, add the route to the listener result
			lr.Routes = append(lr.Routes, r.GetRouteChain(kctx, ctx, route, hostnames, ref))
		}

		// Handle route errors based on checks
		if !anyRoutesAllowed {
			ret.RouteErrors = append(ret.RouteErrors, &RouteError{
				Route:     route,
				ParentRef: ref,
				Error:     Error{E: ErrNotAllowedByListeners, Reason: gwv1.RouteReasonNotAllowedByListeners},
			})
		} else if !anyListenerMatched {
			ret.RouteErrors = append(ret.RouteErrors, &RouteError{
				Route:     route,
				ParentRef: ref,
				Error:     Error{E: ErrNoMatchingParent, Reason: gwv1.RouteReasonNoMatchingParent},
			})
		} else if routeKind == wellknown.HTTPRouteKind && !anyHostsMatch {
			ret.RouteErrors = append(ret.RouteErrors, &RouteError{
				Route:     route,
				ParentRef: ref,
				Error:     Error{E: ErrNoMatchingListenerHostname, Reason: gwv1.RouteReasonNoMatchingListenerHostname},
			})
		}
	}

	return nil
}

// isKindAllowed is a helper function to check if a kind is allowed.
func isKindAllowed(routeKind string, allowedKinds []metav1.GroupKind) bool {
	for _, kind := range allowedKinds {
		if kind.Kind == routeKind {
			return true
		}
	}
	return false
}

type Namespaced interface {
	GetName() string
	GetNamespace() string
}

func namespacedName(o Namespaced) types.NamespacedName {
	return types.NamespacedName{Name: o.GetName(), Namespace: o.GetNamespace()}
}
