package krtcollections

import (
	"errors"
	"fmt"
	"slices"

	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/ptr"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
	gwv1a2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	extensionsplug "github.com/kgateway-dev/kgateway/v2/internal/kgateway/extensions2/plugin"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/ir"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/translator/backendref"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/utils/krtutil"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/wellknown"
)

var (
	ErrMissingReferenceGrant = errors.New("missing reference grant")
	ErrUnknownBackendKind    = errors.New("unknown backend kind")
)

type NotFoundError struct {
	// I call this `NotFound` so its easy to find in krt dump.
	NotFoundObj ir.ObjectSource
}

func (n *NotFoundError) Error() string {
	return fmt.Sprintf("%s \"%s\" not found", n.NotFoundObj.Kind, n.NotFoundObj.Name)
}

// MARK: BackendIndex

type BackendIndex struct {
	// availableBackends maps from the GroupKind of the backend providing plugin that
	// supplied these backendObjs to a collection of BackendObjIRs that have all attached policies pre-computed
	availableBackends           map[schema.GroupKind]krt.Collection[ir.BackendObjectIR]
	availableBackendsWithPolicy []krt.Collection[ir.BackendObjectIR]
	backendRefExtension         []extensionsplug.GetBackendForRefPlugin
	policies                    *PolicyIndex
	refgrants                   *RefGrantIndex
	krtopts                     krtutil.KrtOptions
}

func NewBackendIndex(
	krtopts krtutil.KrtOptions,
	backendRefExtension []extensionsplug.GetBackendForRefPlugin,
	policies *PolicyIndex,
	refgrants *RefGrantIndex,
) *BackendIndex {
	return &BackendIndex{
		policies:            policies,
		refgrants:           refgrants,
		availableBackends:   map[schema.GroupKind]krt.Collection[ir.BackendObjectIR]{},
		krtopts:             krtopts,
		backendRefExtension: backendRefExtension,
	}
}

func (i *BackendIndex) HasSynced() bool {
	if !i.policies.HasSynced() {
		return false
	}
	if !i.refgrants.HasSynced() {
		return false
	}
	for _, col := range i.availableBackends {
		if !col.HasSynced() {
			return false
		}
	}
	return true
}

func (i *BackendIndex) BackendsWithPolicy() []krt.Collection[ir.BackendObjectIR] {
	return i.availableBackendsWithPolicy
}

// AddBackends builds the backends stored in this BackendIndex by deriving a new BackendObjIR collection
// based on the provided `col` with all Backend-attached policies included on the new BackendObjIR.
// The BackendIndex will then store this collection of backendWithPolicies in its internal map, keyed by the
// provied gk. I.e. for the provided gk, it will carry the collection of backends derived from it, with all
// policies attached.
func (i *BackendIndex) AddBackends(gk schema.GroupKind, col krt.Collection[ir.BackendObjectIR]) {
	backendsWithPoliciesCol := krt.NewCollection(col, func(kctx krt.HandlerContext, backendObj ir.BackendObjectIR) *ir.BackendObjectIR {
		policies := i.policies.getTargetingPoliciesForBackends(kctx, extensionsplug.BackendAttachmentPoint, backendObj.ObjectSource, "")
		backendObj.AttachedPolicies = toAttachedPolicies(policies)
		return &backendObj
	}, i.krtopts.ToOptions("")...)
	i.availableBackends[gk] = col
	i.availableBackendsWithPolicy = append(i.availableBackendsWithPolicy, backendsWithPoliciesCol)
}

// if we want to make this function public, make it do ref grants
func (i *BackendIndex) getBackend(kctx krt.HandlerContext, gk schema.GroupKind, n types.NamespacedName, gwport *gwv1.PortNumber) (*ir.BackendObjectIR, error) {
	key := ir.ObjectSource{
		Group:     emptyIfCore(gk.Group),
		Kind:      gk.Kind,
		Namespace: n.Namespace,
		Name:      n.Name,
	}

	var port int32
	if gwport != nil {
		port = int32(*gwport)
	}

	for _, getBackendRcol := range i.backendRefExtension {
		if up := getBackendRcol(kctx, key, port); up != nil {
			return up, nil
		}
	}

	col := i.availableBackends[gk]
	if col == nil {
		return nil, ErrUnknownBackendKind
	}

	up := krt.FetchOne(kctx, col, krt.FilterKey(ir.BackendResourceName(key, port, "")))
	if up == nil {
		return nil, &NotFoundError{NotFoundObj: key}
	}
	return up, nil
}

func (i *BackendIndex) getBackendFromRef(kctx krt.HandlerContext, localns string, ref gwv1.BackendObjectReference) (*ir.BackendObjectIR, error) {
	resolved := toFromBackendRef(localns, ref)
	return i.getBackend(kctx, resolved.GetGroupKind(), types.NamespacedName{Namespace: resolved.Namespace, Name: resolved.Name}, ref.Port)
}

func (i *BackendIndex) GetBackendFromRef(kctx krt.HandlerContext, src ir.ObjectSource, ref gwv1.BackendObjectReference) (*ir.BackendObjectIR, error) {
	fromns := src.Namespace

	fromgk := schema.GroupKind{
		Group: src.Group,
		Kind:  src.Kind,
	}
	to := toFromBackendRef(fromns, ref)

	if i.refgrants.ReferenceAllowed(kctx, fromgk, fromns, to) {
		return i.getBackendFromRef(kctx, src.Namespace, ref)
	} else {
		return nil, ErrMissingReferenceGrant
	}
}

// MARK: GatewayIndex

type GatewayIndex struct {
	policies *PolicyIndex
	Gateways krt.Collection[ir.Gateway]
}

func NewGatewayIndex(
	krtopts krtutil.KrtOptions,
	controllerName string,
	policies *PolicyIndex,
	gws krt.Collection[*gwv1.Gateway],
	gwClasses krt.Collection[*gwv1.GatewayClass],
) *GatewayIndex {
	h := &GatewayIndex{policies: policies}

	h.Gateways = krt.NewCollection(gws, func(kctx krt.HandlerContext, i *gwv1.Gateway) *ir.Gateway {
		// only care about gateways use a class controlled by us
		gwClass := ptr.Flatten(krt.FetchOne(kctx, gwClasses, krt.FilterKey(string(i.Spec.GatewayClassName))))
		if gwClass == nil || controllerName != string(gwClass.Spec.ControllerName) {
			return nil
		}

		out := ir.Gateway{
			ObjectSource: ir.ObjectSource{
				Group:     gwv1.SchemeGroupVersion.Group,
				Kind:      "Gateway",
				Namespace: i.Namespace,
				Name:      i.Name,
			},
			Obj:       i,
			Listeners: make([]ir.Listener, 0, len(i.Spec.Listeners)),
		}

		// TODO: http polic
		//		panic("TODO: implement http policies not just listener")
		out.AttachedListenerPolicies = toAttachedPolicies(h.policies.getTargetingPolicies(kctx, extensionsplug.GatewayAttachmentPoint, out.ObjectSource, ""))
		out.AttachedHttpPolicies = out.AttachedListenerPolicies // see if i can find a better way to segment the listener level and http level policies
		for _, l := range i.Spec.Listeners {
			out.Listeners = append(out.Listeners, ir.Listener{
				Listener:         l,
				AttachedPolicies: toAttachedPolicies(h.policies.getTargetingPolicies(kctx, extensionsplug.RouteAttachmentPoint, out.ObjectSource, string(l.Name))),
			})
		}

		return &out
	}, krtopts.ToOptions("gateways")...)
	return h
}

type targetRefIndexKey struct {
	ir.PolicyRef
	Namespace string
}

func (k targetRefIndexKey) String() string {
	return fmt.Sprintf("%s/%s/%s/%s", k.Group, k.Kind, k.Name, k.Namespace)
}

type globalPolicy struct {
	schema.GroupKind
	ir     func(krt.HandlerContext, extensionsplug.AttachmentPoints) ir.PolicyIR
	points extensionsplug.AttachmentPoints
}

// MARK: PolicyIndex
type policyAndIndex struct {
	policies            krt.Collection[ir.PolicyWrapper]
	policiesByTargetRef krt.Collection[ir.PolicyWrapper]
	index               krt.Index[targetRefIndexKey, ir.PolicyWrapper]
	forBackends         bool
}
type PolicyIndex struct {
	availablePolicies map[schema.GroupKind]policyAndIndex

	policiesFetch  map[schema.GroupKind]func(n string, ns string) ir.PolicyIR
	globalPolicies []globalPolicy

	hasSyncedFuncs []func() bool
}
type policyFetcherMap = map[schema.GroupKind]func(n string, ns string) ir.PolicyIR

func (h *PolicyIndex) HasSynced() bool {
	for _, f := range h.hasSyncedFuncs {
		if !f() {
			return false
		}
	}
	for _, pi := range h.availablePolicies {
		if !pi.policies.HasSynced() {
			return false
		}
		if !pi.policiesByTargetRef.HasSynced() {
			return false
		}
	}
	return true
}

func NewPolicyIndex(krtopts krtutil.KrtOptions, contributesPolicies extensionsplug.ContributesPolicies) *PolicyIndex {
	index := &PolicyIndex{policiesFetch: policyFetcherMap{}, availablePolicies: map[schema.GroupKind]policyAndIndex{}}

	for gk, plugin := range contributesPolicies {
		if plugin.Policies != nil {
			policies := plugin.Policies
			forBackends := plugin.ProcessBackend != nil
			policiesByTargetRef := krt.NewCollection(policies, func(kctx krt.HandlerContext, a ir.PolicyWrapper) *ir.PolicyWrapper {
				if len(a.TargetRefs) == 0 {
					return nil
				}
				return &a
			}, krtopts.ToOptions(fmt.Sprintf("%s-policiesByTargetRef", gk.String()))...)

			targetRefIndex := krt.NewIndex(policiesByTargetRef, func(p ir.PolicyWrapper) []targetRefIndexKey {
				ret := make([]targetRefIndexKey, len(p.TargetRefs))
				for i, tr := range p.TargetRefs {
					ret[i] = targetRefIndexKey{
						PolicyRef: tr,
						Namespace: p.Namespace,
					}
				}
				return ret
			})

			index.availablePolicies[gk] = policyAndIndex{
				policies:            policies,
				policiesByTargetRef: policiesByTargetRef,
				index:               targetRefIndex,
				forBackends:         forBackends,
			}
			index.hasSyncedFuncs = append(index.hasSyncedFuncs, plugin.Policies.HasSynced)
		}
		if plugin.PoliciesFetch != nil {
			index.policiesFetch[gk] = plugin.PoliciesFetch
		}
		if plugin.GlobalPolicies != nil {
			index.globalPolicies = append(index.globalPolicies, globalPolicy{
				GroupKind: gk,
				ir:        plugin.GlobalPolicies,
				points:    plugin.AttachmentPoints(),
			})
		}
	}

	return index
}
func (p *PolicyIndex) fetchByTargetRef(
	kctx krt.HandlerContext,
	targetRef targetRefIndexKey,
	onlyBackends bool,
) []ir.PolicyWrapper {
	var ret []ir.PolicyWrapper
	for _, policyCol := range p.availablePolicies {
		policies := krt.Fetch(kctx, policyCol.policiesByTargetRef, krt.FilterIndex(policyCol.index, targetRef))
		if onlyBackends && !policyCol.forBackends {
			continue
		}
		ret = append(ret, policies...)
	}
	return ret
}

// Attachment happens during collection creation (i.e. this file), and not translation. so these methods don't need to be public!
// note: we may want to change that for global policies maybe.

func (p *PolicyIndex) getTargetingPoliciesForBackends(
	kctx krt.HandlerContext,
	pnt extensionsplug.AttachmentPoints,
	targetRef ir.ObjectSource,
	sectionName string,
) []ir.PolicyAtt {
	return p.getTargetingPoliciesMaybeForBackends(kctx, pnt, targetRef, sectionName, true)
}

func (p *PolicyIndex) getTargetingPolicies(
	kctx krt.HandlerContext,
	pnt extensionsplug.AttachmentPoints,
	targetRef ir.ObjectSource,
	sectionName string,
) []ir.PolicyAtt {
	return p.getTargetingPoliciesMaybeForBackends(kctx, pnt, targetRef, sectionName, false)
}

func (p *PolicyIndex) getTargetingPoliciesMaybeForBackends(
	kctx krt.HandlerContext,
	pnt extensionsplug.AttachmentPoints,
	targetRef ir.ObjectSource,
	sectionName string,
	onlyBackends bool,
) []ir.PolicyAtt {
	var ret []ir.PolicyAtt
	for _, gp := range p.globalPolicies {
		if gp.points.Has(pnt) {
			if p := gp.ir(kctx, pnt); p != nil {
				gpAtt := ir.PolicyAtt{
					PolicyIr:  p,
					GroupKind: gp.GroupKind,
				}
				ret = append(ret, gpAtt)
			}
		}
	}

	// no need for ref grants here as target refs are namespace local
	targetRefIndexKey := targetRefIndexKey{
		PolicyRef: ir.PolicyRef{
			Group: targetRef.Group,
			Kind:  targetRef.Kind,
			Name:  targetRef.Name,
		},
		Namespace: targetRef.Namespace,
	}
	policies := p.fetchByTargetRef(kctx, targetRefIndexKey, onlyBackends)
	var sectionNamePolicies []ir.PolicyWrapper
	if sectionName != "" {
		targetRefIndexKey.SectionName = sectionName
		sectionNamePolicies = p.fetchByTargetRef(kctx, targetRefIndexKey, onlyBackends)
	}

	for _, p := range policies {
		ret = append(ret, ir.PolicyAtt{
			GroupKind: p.GetGroupKind(),
			PolicyIr:  p.PolicyIR,
			PolicyRef: &ir.AttachedPolicyRef{
				Group:     p.Group,
				Kind:      p.Kind,
				Name:      p.Name,
				Namespace: p.Namespace,
			},
			Errors: p.Errors,
		})
	}
	for _, p := range sectionNamePolicies {
		ret = append(ret, ir.PolicyAtt{
			GroupKind: p.GetGroupKind(),
			PolicyIr:  p.PolicyIR,
			PolicyRef: &ir.AttachedPolicyRef{
				Group:       p.Group,
				Kind:        p.Kind,
				Name:        p.Name,
				Namespace:   p.Namespace,
				SectionName: sectionName,
			},
			Errors: p.Errors,
		})
	}
	slices.SortFunc(ret, func(a, b ir.PolicyAtt) int {
		return a.PolicyIr.CreationTime().Compare(b.PolicyIr.CreationTime())
	})
	return ret
}

func (p *PolicyIndex) fetchPolicy(kctx krt.HandlerContext, policyRef ir.ObjectSource) *ir.PolicyWrapper {
	gk := policyRef.GetGroupKind()
	if f, ok := p.policiesFetch[gk]; ok {
		if polIr := f(policyRef.Name, policyRef.Namespace); polIr != nil {
			return &ir.PolicyWrapper{PolicyIR: polIr}
		}
	}
	if pi, ok := p.availablePolicies[gk]; ok {
		return krt.FetchOne(kctx, pi.policies, krt.FilterKey(policyRef.ResourceName()))
	}
	return nil
}

type refGrantIndexKey struct {
	RefGrantNs string
	ToGK       schema.GroupKind
	ToName     string
	FromGK     schema.GroupKind
	FromNs     string
}

func (k refGrantIndexKey) String() string {
	return fmt.Sprintf("%s/%s/%s/%s/%s/%s/%s", k.RefGrantNs, k.FromNs, k.ToGK.Group, k.ToGK.Kind, k.ToName, k.FromGK.Group, k.FromGK.Kind)
}

// MARK: RefGrantIndex

type RefGrantIndex struct {
	refgrants     krt.Collection[*gwv1beta1.ReferenceGrant]
	refGrantIndex krt.Index[refGrantIndexKey, *gwv1beta1.ReferenceGrant]
}

func (h *RefGrantIndex) HasSynced() bool {
	return h.refgrants.HasSynced()
}

func NewRefGrantIndex(refgrants krt.Collection[*gwv1beta1.ReferenceGrant]) *RefGrantIndex {
	refGrantIndex := krt.NewIndex(refgrants, func(p *gwv1beta1.ReferenceGrant) []refGrantIndexKey {
		ret := make([]refGrantIndexKey, 0, len(p.Spec.To)*len(p.Spec.From))
		for _, from := range p.Spec.From {
			for _, to := range p.Spec.To {
				ret = append(ret, refGrantIndexKey{
					RefGrantNs: p.Namespace,
					ToGK:       schema.GroupKind{Group: emptyIfCore(string(to.Group)), Kind: string(to.Kind)},
					ToName:     strOr(to.Name, ""),
					FromGK:     schema.GroupKind{Group: emptyIfCore(string(from.Group)), Kind: string(from.Kind)},
					FromNs:     string(from.Namespace),
				})
			}
		}
		return ret
	})
	return &RefGrantIndex{refgrants: refgrants, refGrantIndex: refGrantIndex}
}

func (r *RefGrantIndex) ReferenceAllowed(kctx krt.HandlerContext, fromgk schema.GroupKind, fromns string, to ir.ObjectSource) bool {
	if fromns == to.Namespace {
		return true
	}
	to.Group = emptyIfCore(to.Group)
	fromgk.Group = emptyIfCore(fromgk.Group)

	key := refGrantIndexKey{
		RefGrantNs: to.Namespace,
		ToGK:       schema.GroupKind{Group: to.Group, Kind: to.Kind},
		FromGK:     fromgk,
		FromNs:     fromns,
	}
	matchingGrants := krt.Fetch(kctx, r.refgrants, krt.FilterIndex(r.refGrantIndex, key))
	if len(matchingGrants) != 0 {
		return true
	}
	// try with name:
	key.ToName = to.Name
	if len(krt.Fetch(kctx, r.refgrants, krt.FilterIndex(r.refGrantIndex, key))) != 0 {
		return true
	}
	return false
}

type RouteWrapper struct {
	Route ir.Route
}

func (c RouteWrapper) ResourceName() string {
	os := ir.ObjectSource{
		Group:     c.Route.GetGroupKind().Group,
		Kind:      c.Route.GetGroupKind().Kind,
		Namespace: c.Route.GetNamespace(),
		Name:      c.Route.GetName(),
	}
	return os.ResourceName()
}

func (c RouteWrapper) Equals(in RouteWrapper) bool {
	switch a := c.Route.(type) {
	case *ir.HttpRouteIR:
		if bhttp, ok := in.Route.(*ir.HttpRouteIR); !ok {
			return false
		} else {
			return a.Equals(*bhttp)
		}
	case *ir.TcpRouteIR:
		if bhttp, ok := in.Route.(*ir.TcpRouteIR); !ok {
			return false
		} else {
			return a.Equals(*bhttp)
		}
	case *ir.TlsRouteIR:
		if bhttp, ok := in.Route.(*ir.TlsRouteIR); !ok {
			return false
		} else {
			return a.Equals(*bhttp)
		}
	}
	panic("unknown route type")
}

// MARK: RoutesIndex

type RoutesIndex struct {
	routes          krt.Collection[RouteWrapper]
	httpRoutes      krt.Collection[ir.HttpRouteIR]
	httpByNamespace krt.Index[string, ir.HttpRouteIR]
	byParentRef     krt.Index[targetRefIndexKey, RouteWrapper]

	policies  *PolicyIndex
	refgrants *RefGrantIndex
	backends  *BackendIndex

	hasSyncedFuncs []func() bool
}

func (h *RoutesIndex) HasSynced() bool {
	for _, f := range h.hasSyncedFuncs {
		if !f() {
			return false
		}
	}
	return h.httpRoutes.HasSynced() && h.routes.HasSynced() && h.policies.HasSynced() && h.backends.HasSynced() && h.refgrants.HasSynced()
}

func NewRoutesIndex(
	krtopts krtutil.KrtOptions,
	httproutes krt.Collection[*gwv1.HTTPRoute],
	tcproutes krt.Collection[*gwv1a2.TCPRoute],
	tlsroutes krt.Collection[*gwv1a2.TLSRoute],
	policies *PolicyIndex,
	backends *BackendIndex,
	refgrants *RefGrantIndex,
) *RoutesIndex {
	h := &RoutesIndex{policies: policies, refgrants: refgrants, backends: backends}
	h.hasSyncedFuncs = append(h.hasSyncedFuncs, httproutes.HasSynced, tcproutes.HasSynced, tlsroutes.HasSynced)
	h.httpRoutes = krt.NewCollection(httproutes, h.transformHttpRoute, krtopts.ToOptions("http-routes-with-policy")...)
	httpRouteCollection := krt.NewCollection(h.httpRoutes, func(kctx krt.HandlerContext, i ir.HttpRouteIR) *RouteWrapper {
		return &RouteWrapper{Route: &i}
	}, krtopts.ToOptions("routes-http-routes-with-policy")...)
	tcpRoutesCollection := krt.NewCollection(tcproutes, func(kctx krt.HandlerContext, i *gwv1a2.TCPRoute) *RouteWrapper {
		t := h.transformTcpRoute(kctx, i)
		return &RouteWrapper{Route: t}
	}, krtopts.ToOptions("routes-tcp-routes-with-policy")...)
	tlsRoutesCollection := krt.NewCollection(tlsroutes, func(kctx krt.HandlerContext, i *gwv1a2.TLSRoute) *RouteWrapper {
		t := h.transformTlsRoute(kctx, i)
		return &RouteWrapper{Route: t}
	}, krtopts.ToOptions("routes-tls-routes-with-policy")...)

	h.routes = krt.JoinCollection([]krt.Collection[RouteWrapper]{httpRouteCollection, tcpRoutesCollection, tlsRoutesCollection}, krtopts.ToOptions("all-routes-with-policy")...)

	httpByNamespace := krt.NewIndex(h.httpRoutes, func(i ir.HttpRouteIR) []string {
		return []string{i.GetNamespace()}
	})
	byParentRef := krt.NewIndex(h.routes, func(in RouteWrapper) []targetRefIndexKey {
		parentRefs := in.Route.GetParentRefs()
		ret := make([]targetRefIndexKey, len(parentRefs))
		for i, pRef := range parentRefs {
			ns := strOr(pRef.Namespace, "")
			if ns == "" {
				ns = in.Route.GetNamespace()
			}
			// HTTPRoute defaults GK to Gateway
			group := wellknown.GatewayGVK.Group
			kind := wellknown.GatewayGVK.Kind
			if pRef.Group != nil {
				group = string(*pRef.Group)
			}
			if pRef.Kind != nil {
				kind = string(*pRef.Kind)
			}
			// lookup by the root object
			ret[i] = targetRefIndexKey{
				Namespace: ns,
				PolicyRef: ir.PolicyRef{
					Group: group,
					Kind:  kind,
					Name:  string(pRef.Name),
					// this index intentionally doesn't include sectionName or port
				},
			}
		}
		return ret
	})
	h.httpByNamespace = httpByNamespace
	h.byParentRef = byParentRef
	return h
}

func (h *RoutesIndex) FetchHttpNamespace(kctx krt.HandlerContext, ns string) []ir.HttpRouteIR {
	return krt.Fetch(kctx, h.httpRoutes, krt.FilterIndex(h.httpByNamespace, ns))
}

func (h *RoutesIndex) RoutesForGateway(kctx krt.HandlerContext, nns types.NamespacedName) []ir.Route {
	return h.RoutesFor(kctx, nns, wellknown.GatewayGVK.Group, wellknown.GatewayGVK.Kind)
}

func (h *RoutesIndex) RoutesFor(kctx krt.HandlerContext, nns types.NamespacedName, group, kind string) []ir.Route {
	rts := krt.Fetch(kctx, h.routes, krt.FilterIndex(h.byParentRef, targetRefIndexKey{
		PolicyRef: ir.PolicyRef{
			Name:  nns.Name,
			Group: group,
			Kind:  kind,
		},
		Namespace: nns.Namespace,
	}))
	ret := make([]ir.Route, len(rts))
	for i, r := range rts {
		ret[i] = r.Route
	}
	return ret
}

func (h *RoutesIndex) FetchHttp(kctx krt.HandlerContext, ns, n string) *ir.HttpRouteIR {
	src := ir.ObjectSource{
		Group:     gwv1.SchemeGroupVersion.Group,
		Kind:      "HTTPRoute",
		Namespace: ns,
		Name:      n,
	}
	route := krt.FetchOne(kctx, h.httpRoutes, krt.FilterKey(src.ResourceName()))
	return route
}

func (h *RoutesIndex) Fetch(kctx krt.HandlerContext, gk schema.GroupKind, ns, n string) *RouteWrapper {
	src := ir.ObjectSource{
		Group:     gk.Group,
		Kind:      gk.Kind,
		Namespace: ns,
		Name:      n,
	}
	return krt.FetchOne(kctx, h.routes, krt.FilterKey(src.ResourceName()))
}

func (h *RoutesIndex) transformTcpRoute(kctx krt.HandlerContext, i *gwv1a2.TCPRoute) *ir.TcpRouteIR {
	src := ir.ObjectSource{
		Group:     gwv1a2.SchemeGroupVersion.Group,
		Kind:      "TCPRoute",
		Namespace: i.Namespace,
		Name:      i.Name,
	}
	var backends []gwv1.BackendRef
	if len(i.Spec.Rules) > 0 {
		backends = i.Spec.Rules[0].BackendRefs
	}
	return &ir.TcpRouteIR{
		ObjectSource:     src,
		SourceObject:     i,
		ParentRefs:       i.Spec.ParentRefs,
		Backends:         h.getTcpBackends(kctx, src, backends),
		AttachedPolicies: toAttachedPolicies(h.policies.getTargetingPolicies(kctx, extensionsplug.RouteAttachmentPoint, src, "")),
	}
}

func (h *RoutesIndex) transformTlsRoute(kctx krt.HandlerContext, i *gwv1a2.TLSRoute) *ir.TlsRouteIR {
	src := ir.ObjectSource{
		Group:     gwv1a2.SchemeGroupVersion.Group,
		Kind:      "TLSRoute",
		Namespace: i.Namespace,
		Name:      i.Name,
	}
	var backends []gwv1.BackendRef
	if len(i.Spec.Rules) > 0 {
		backends = i.Spec.Rules[0].BackendRefs
	}
	return &ir.TlsRouteIR{
		ObjectSource:     src,
		SourceObject:     i,
		ParentRefs:       i.Spec.ParentRefs,
		Backends:         h.getTcpBackends(kctx, src, backends),
		Hostnames:        tostr(i.Spec.Hostnames),
		AttachedPolicies: toAttachedPolicies(h.policies.getTargetingPolicies(kctx, extensionsplug.RouteAttachmentPoint, src, "")),
	}
}

func (h *RoutesIndex) transformHttpRoute(kctx krt.HandlerContext, i *gwv1.HTTPRoute) *ir.HttpRouteIR {
	src := ir.ObjectSource{
		Group:     gwv1.SchemeGroupVersion.Group,
		Kind:      "HTTPRoute",
		Namespace: i.Namespace,
		Name:      i.Name,
	}

	return &ir.HttpRouteIR{
		ObjectSource:     src,
		SourceObject:     i,
		ParentRefs:       i.Spec.ParentRefs,
		Hostnames:        tostr(i.Spec.Hostnames),
		Rules:            h.transformRules(kctx, src, i.Spec.Rules),
		AttachedPolicies: toAttachedPolicies(h.policies.getTargetingPolicies(kctx, extensionsplug.RouteAttachmentPoint, src, "")),
	}
}

func (h *RoutesIndex) transformRules(
	kctx krt.HandlerContext,
	src ir.ObjectSource,
	i []gwv1.HTTPRouteRule,
) []ir.HttpRouteRuleIR {
	rules := make([]ir.HttpRouteRuleIR, 0, len(i))
	for _, r := range i {
		extensionRefs := h.getExtensionRefs(kctx, src.Namespace, r.Filters)
		var policies ir.AttachedPolicies
		if r.Name != nil {
			policies = toAttachedPolicies(h.policies.getTargetingPolicies(kctx, extensionsplug.RouteAttachmentPoint, src, string(*r.Name)))
		}

		rules = append(rules, ir.HttpRouteRuleIR{
			ExtensionRefs:    extensionRefs,
			AttachedPolicies: policies,
			Backends:         h.getBackends(kctx, src, r.BackendRefs),
			Matches:          r.Matches,
			Name:             emptyIfNil(r.Name),
			Timeouts:         r.Timeouts,
		})
	}
	return rules
}

func (h *RoutesIndex) getExtensionRefs(kctx krt.HandlerContext, ns string, r []gwv1.HTTPRouteFilter) ir.AttachedPolicies {
	ret := ir.AttachedPolicies{
		Policies: map[schema.GroupKind][]ir.PolicyAtt{},
	}
	for _, ext := range r {
		// TODO: propagate error if we can't find the extension
		gk, policy := h.resolveExtension(kctx, ns, ext)
		if policy != nil {
			ret.Policies[gk] = append(ret.Policies[gk], ir.PolicyAtt{PolicyIr: policy /*direct attachment - no target ref*/})
		}
	}
	return ret
}

func (h *RoutesIndex) resolveExtension(kctx krt.HandlerContext, ns string, ext gwv1.HTTPRouteFilter) (schema.GroupKind, ir.PolicyIR) {
	if ext.Type == gwv1.HTTPRouteFilterExtensionRef {
		if ext.ExtensionRef == nil {
			// TODO: report error!!
			return schema.GroupKind{}, nil
		}
		ref := *ext.ExtensionRef
		key := ir.ObjectSource{
			Group:     string(ref.Group),
			Kind:      string(ref.Kind),
			Namespace: ns,
			Name:      string(ref.Name),
		}
		policy := h.policies.fetchPolicy(kctx, key)
		if policy == nil {
			// TODO: report error!!
			return schema.GroupKind{}, nil
		}

		gk := schema.GroupKind{
			Group: string(ref.Group),
			Kind:  string(ref.Kind),
		}
		return gk, policy.PolicyIR
	}

	fromGK := schema.GroupKind{
		Group: gwv1.SchemeGroupVersion.Group,
		Kind:  "HTTPRoute",
	}

	return VirtualBuiltInGK, NewBuiltInIr(kctx, ext, fromGK, ns, h.refgrants, h.backends)
}

func toFromBackendRef(fromns string, ref gwv1.BackendObjectReference) ir.ObjectSource {
	return ir.ObjectSource{
		Group:     strOr(ref.Group, ""),
		Kind:      strOr(ref.Kind, "Service"),
		Namespace: strOr(ref.Namespace, fromns),
		Name:      string(ref.Name),
	}
}

func (h *RoutesIndex) getBackends(kctx krt.HandlerContext, src ir.ObjectSource, backendRefs []gwv1.HTTPBackendRef) []ir.HttpBackendOrDelegate {
	backends := make([]ir.HttpBackendOrDelegate, 0, len(backendRefs))
	for _, ref := range backendRefs {
		extensionRefs := h.getExtensionRefs(kctx, src.Namespace, ref.Filters)
		fromns := src.Namespace

		to := toFromBackendRef(fromns, ref.BackendObjectReference)
		if backendref.RefIsHTTPRoute(ref.BackendRef.BackendObjectReference) {
			backends = append(backends, ir.HttpBackendOrDelegate{
				Delegate:         &to,
				AttachedPolicies: extensionRefs,
			})
			continue
		}

		backend, err := h.backends.GetBackendFromRef(kctx, src, ref.BackendRef.BackendObjectReference)

		// TODO: if we can't find the backend, should we
		// still use its cluster name in case it comes up later?
		// if so we need to think about the way create cluster names,
		// so it only depends on the backend-ref
		clusterName := "blackhole-cluster"
		if backend != nil {
			clusterName = backend.ClusterName()
		} else if err == nil {
			err = &NotFoundError{NotFoundObj: to}
		}
		backends = append(backends, ir.HttpBackendOrDelegate{
			Backend: &ir.BackendRefIR{
				BackendObject: backend,
				ClusterName:   clusterName,
				Weight:        weight(ref.Weight),
				Err:           err,
			},
			AttachedPolicies: extensionRefs,
		})
	}
	return backends
}

func (h *RoutesIndex) getTcpBackends(kctx krt.HandlerContext, src ir.ObjectSource, i []gwv1.BackendRef) []ir.BackendRefIR {
	backends := make([]ir.BackendRefIR, 0, len(i))
	for _, ref := range i {
		backend, err := h.backends.GetBackendFromRef(kctx, src, ref.BackendObjectReference)
		clusterName := "blackhole-cluster"
		if backend != nil {
			clusterName = backend.ClusterName()
		} else if err == nil {
			err = &NotFoundError{NotFoundObj: toFromBackendRef(src.Namespace, ref.BackendObjectReference)}
		}
		backends = append(backends, ir.BackendRefIR{
			BackendObject: backend,
			ClusterName:   clusterName,
			Weight:        weight(ref.Weight),
			Err:           err,
		})
	}
	return backends
}

func strOr[T ~string](s *T, def string) string {
	if s == nil {
		return def
	}
	return string(*s)
}

func weight(w *int32) uint32 {
	if w == nil {
		return 1
	}
	return uint32(*w)
}

func toAttachedPolicies(policies []ir.PolicyAtt) ir.AttachedPolicies {
	ret := ir.AttachedPolicies{
		Policies: map[schema.GroupKind][]ir.PolicyAtt{},
	}
	for _, p := range policies {
		gk := schema.GroupKind{
			Group: p.GroupKind.Group,
			Kind:  p.GroupKind.Kind,
		}
		// TODO: do not create a new PolicyAtt, just use existing `p`
		polAtt := ir.PolicyAtt{
			PolicyIr:  p.PolicyIr,
			PolicyRef: p.PolicyRef,
			GroupKind: gk,
			Errors:    p.Errors,
		}
		ret.Policies[gk] = append(ret.Policies[gk], polAtt)
	}
	return ret
}

func emptyIfNil(s *gwv1.SectionName) string {
	if s == nil {
		return ""
	}
	return string(*s)
}

func tostr(in []gwv1.Hostname) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	for i, h := range in {
		out[i] = string(h)
	}
	return out
}

func emptyIfCore(s string) string {
	if s == "core" {
		return ""
	}
	return s
}
