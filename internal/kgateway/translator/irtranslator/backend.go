package irtranslator

import (
	"context"
	"errors"
	"time"

	clusterv3 "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	envoy_config_core_v3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	envoy_upstreams_v3 "github.com/envoyproxy/go-control-plane/envoy/extensions/upstreams/http/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"istio.io/istio/pkg/kube/krt"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/extensions2/common"
	extensionsplug "github.com/kgateway-dev/kgateway/v2/internal/kgateway/extensions2/plugin"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/ir"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/utils"
)

var (
	ClusterConnectionTimeout = time.Second * 5
)

type BackendTranslator struct {
	ContributedBackends map[schema.GroupKind]ir.BackendInit
	ContributedPolicies map[schema.GroupKind]extensionsplug.PolicyPlugin
	CommonCols          *common.CommonCollections
}

func (t *BackendTranslator) TranslateBackend(
	kctx krt.HandlerContext,
	ucc ir.UniqlyConnectedClient,
	backend ir.BackendObjectIR,
) (*clusterv3.Cluster, error) {
	gk := schema.GroupKind{
		Group: backend.Group,
		Kind:  backend.Kind,
	}
	process, ok := t.ContributedBackends[gk]
	if !ok {
		return nil, errors.New("no backend translator found for " + gk.String())
	}

	if process.InitBackend == nil {
		return nil, errors.New("no backend plugin found for " + gk.String())
	}

	if backend.Errors != nil {
		// the backend has errors so we can't translate our real cluster
		// so return a blackhole cluster instead. (in case a consumer attempts to use it)
		// also return the errors to signify to callers it's not a dev error but a real error
		// from backend object translation.
		// this cluster will ultimately be dropped before it added to the xDS snapshot
		// see: internal/kgateway/proxy_syncer/perclient.go
		out := buildBlackholeCluster(&backend)
		return out, errors.Join(backend.Errors...)
	}

	out := initializeCluster(backend)
	process.InitBackend(context.TODO(), backend, out)
	processDnsLookupFamily(out, t.CommonCols)

	// now process backend policies
	t.runPolicies(kctx, context.TODO(), ucc, backend, out)
	return out, nil
}

func (t *BackendTranslator) runPolicies(
	kctx krt.HandlerContext,
	ctx context.Context,
	ucc ir.UniqlyConnectedClient,
	backend ir.BackendObjectIR,
	out *clusterv3.Cluster,
) {
	for gk, policyPlugin := range t.ContributedPolicies {
		// TODO: in theory it would be nice to do `ProcessBackend` once, and only do
		// the the per-client processing for each client.
		// that would require refactoring and thinking about the proper IR, so we'll punt on that for
		// now, until we have more backend plugin examples to properly understand what it should look
		// like.
		if policyPlugin.PerClientProcessBackend != nil {
			policyPlugin.PerClientProcessBackend(kctx, ctx, ucc, backend, out)
		}

		if policyPlugin.ProcessBackend == nil {
			continue
		}
		for _, polAttachment := range backend.AttachedPolicies.Policies[gk] {
			policyPlugin.ProcessBackend(ctx, polAttachment.PolicyIr, backend, out)
		}
	}
}

var (
	h2Options = func() *anypb.Any {
		http2ProtocolOptions := &envoy_upstreams_v3.HttpProtocolOptions{
			UpstreamProtocolOptions: &envoy_upstreams_v3.HttpProtocolOptions_ExplicitHttpConfig_{
				ExplicitHttpConfig: &envoy_upstreams_v3.HttpProtocolOptions_ExplicitHttpConfig{
					ProtocolConfig: &envoy_upstreams_v3.HttpProtocolOptions_ExplicitHttpConfig_Http2ProtocolOptions{
						Http2ProtocolOptions: &envoy_config_core_v3.Http2ProtocolOptions{},
					},
				},
			},
		}

		a, err := utils.MessageToAny(http2ProtocolOptions)
		if err != nil {
			// should never happen - all values are known ahead of time.
			panic(err)
		}
		return a
	}()
)

// processDnsLookupFamily modifies clusters that use DNS-based discovery in the following way:
// 1. explicitly default to 'V4_PREFERRED' (as opposed to the envoy default of effectively V6_PREFERRED)
// 2. override to value defined in kgateway global setting if present
func processDnsLookupFamily(out *clusterv3.Cluster, cc *common.CommonCollections) {
	cdt, ok := out.GetClusterDiscoveryType().(*clusterv3.Cluster_Type)
	if !ok {
		return
	}
	setDns := false
	switch cdt.Type {
	case clusterv3.Cluster_STATIC, clusterv3.Cluster_LOGICAL_DNS, clusterv3.Cluster_STRICT_DNS:
		setDns = true
	}
	if !setDns {
		return
	}

	// irrespective of settings, default to V4_PREFERRED, overriding Envoy default
	out.DnsLookupFamily = clusterv3.Cluster_V4_PREFERRED

	if cc == nil {
		return
	}
	// if we have settings, use value from it
	switch cc.Settings.DnsLookupFamily {
	case "V4_PREFERRED":
		out.DnsLookupFamily = clusterv3.Cluster_V4_PREFERRED
	case "V4_ONLY":
		out.DnsLookupFamily = clusterv3.Cluster_V4_ONLY
	case "V6_ONLY":
		out.DnsLookupFamily = clusterv3.Cluster_V6_ONLY
	case "AUTO":
		out.DnsLookupFamily = clusterv3.Cluster_AUTO
	case "ALL":
		out.DnsLookupFamily = clusterv3.Cluster_ALL
	}
}

func translateAppProtocol(appProtocol ir.AppProtocol) map[string]*anypb.Any {
	typedExtensionProtocolOptions := map[string]*anypb.Any{}
	switch appProtocol {
	case ir.HTTP2AppProtocol:
		typedExtensionProtocolOptions["envoy.extensions.upstreams.http.v3.HttpProtocolOptions"] = proto.Clone(h2Options).(*anypb.Any)
	}
	return typedExtensionProtocolOptions
}

// initializeCluster creates a default envoy cluster with minimal configuration,
// that will then be augmented by various backend plugins
func initializeCluster(u ir.BackendObjectIR) *clusterv3.Cluster {
	// circuitBreakers := t.settings.GetGloo().GetCircuitBreakers()
	out := &clusterv3.Cluster{
		Name:     u.ClusterName(),
		Metadata: new(envoy_config_core_v3.Metadata),
		//	CircuitBreakers:  getCircuitBreakers(upstream.GetCircuitBreakers(), circuitBreakers),
		//	LbSubsetConfig:   createLbConfig(upstream),
		//	HealthChecks:     hcConfig,
		//		OutlierDetection: detectCfg,
		// defaults to Cluster_USE_CONFIGURED_PROTOCOL
		// ProtocolSelection: envoy_config_cluster_v3.Cluster_ClusterProtocolSelection(upstream.GetProtocolSelection()),
		// this field can be overridden by plugins
		ConnectTimeout:                durationpb.New(ClusterConnectionTimeout),
		TypedExtensionProtocolOptions: translateAppProtocol(u.AppProtocol),

		// Http2ProtocolOptions:      getHttp2options(upstream),
		// IgnoreHealthOnHostRemoval: upstream.GetIgnoreHealthOnHostRemoval().GetValue(),
		//	RespectDnsTtl:             upstream.GetRespectDnsTtl().GetValue(),
		//	DnsRefreshRate:            dnsRefreshRate,
		//	PreconnectPolicy:          preconnect,
	}

	// proxyprotocol may be wiped by some plugins that transform transport sockets
	// see static and failover at time of writing.
	//	if upstream.GetProxyProtocolVersion() != nil {
	//
	//		tp, err := upstream_proxy_protocol.WrapWithPProtocol(out.GetTransportSocket(), upstream.GetProxyProtocolVersion().GetValue())
	//		if err != nil {
	//			errorList = append(errorList, err)
	//		} else {
	//			out.TransportSocket = tp
	//		}
	//	}
	//
	return out
}

func buildBlackholeCluster(u *ir.BackendObjectIR) *clusterv3.Cluster {
	out := &clusterv3.Cluster{
		Name:     u.ClusterName(),
		Metadata: new(envoy_config_core_v3.Metadata),
		ClusterDiscoveryType: &clusterv3.Cluster_Type{
			Type: clusterv3.Cluster_STATIC,
		},
		LoadAssignment: &endpointv3.ClusterLoadAssignment{
			ClusterName: u.ClusterName(),
			Endpoints:   []*endpointv3.LocalityLbEndpoints{},
		},
	}
	return out
}
