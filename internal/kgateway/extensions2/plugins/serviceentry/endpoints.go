package serviceentry

import (
	"context"
	"fmt"
	"net"

	"google.golang.org/protobuf/types/known/wrapperspb"

	networking "istio.io/api/networking/v1alpha3"
	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pkg/kube/krt"

	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	"github.com/solo-io/go-utils/contextutils"

	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/endpoints"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/ir"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/krtcollections"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/utils/krtutil"
)

// buildInlineCLA creates a CLA for non-EDS ServiceEntry.
// TODO it would be nice to re-use the EndpointsForBackend collection to handle this.
// rather than doing it as part of `initBackend` which doesn't have a way to apply
// DestinationRule (or any per-client policy) properly.
func buildInlineCLA(ctx context.Context, be ir.BackendObjectIR, se *networkingclient.ServiceEntry) *endpointv3.ClusterLoadAssignment {
	logger := contextutils.LoggerFrom(ctx)

	var inlineWorkloads []selectedWorkload
	for i, e := range se.Spec.GetEndpoints() {
		converted := selectedWorkloadFromEntry(
			fmt.Sprintf("%s-endpoints-%d", se.Name, i), // synthetic name
			se.Namespace, // inherit se namespace
			nil,          // no metadata labels to inherit
			e,
			nil, // not in krt, don't need selectedBy
		)
		inlineWorkloads = append(inlineWorkloads, converted)
	}

	// DNS resolution can use the hosts field if endpoints are empty
	// https://istio.io/latest/docs/reference/config/networking/service-entry/#ServiceEntry-Resolution
	if isDNSServiceEntry(se) && len(inlineWorkloads) == 0 {
		for i, hostname := range se.Spec.GetHosts() {
			converted := selectedWorkloadFromEntry(
				fmt.Sprintf("%s-hosts-%d", se.Name, i), // synthetic name
				se.Namespace,                           // inherit se namespace
				nil,                                    // no metadata labels to inherit
				&networking.WorkloadEntry{Address: hostname},
				nil, // not in krt, don't need selectedBy
			)
			inlineWorkloads = append(inlineWorkloads, converted)
		}
	}

	endpointsForBackend := endpointsFromWorkloads(se, be, inlineWorkloads)
	if endpointsForBackend == nil {
		// this is pretty much impossible, but `ir.NewEndpointsForBackend(be)`
		// returns a pointer, so this is for safety
		logger.DPanicw("buildInlineCLA for ServiceEntry had nil endpointsForBackend", "ServiceEntry", krt.NewNamed(se).ResourceName())
		return nil
	}

	return endpoints.PrioritizeEndpoints(
		logger.Desugar(),
		nil,
		*endpointsForBackend,
		ir.UniqlyConnectedClient{},
	)
}

func endpointsCollection(
	Backends krt.Collection[ir.BackendObjectIR],
	SelectedWorkloads krt.Collection[selectedWorkload],
	selectedWorkloadsIndex krt.Index[string, selectedWorkload],
	krtOpts krtutil.KrtOptions,
) krt.Collection[ir.EndpointsForBackend] {
	return krt.NewCollection(
		Backends,
		func(ctx krt.HandlerContext, be ir.BackendObjectIR) *ir.EndpointsForBackend {
			se, ok := be.Obj.(*networkingclient.ServiceEntry)
			if !ok {
				return nil
			}
			if !isEDSServiceEntry(se) {
				return nil
			}
			workloads := krt.Fetch(ctx, SelectedWorkloads, krt.FilterIndex(selectedWorkloadsIndex, serviceEntryKey(se)))

			return endpointsFromWorkloads(se, be, workloads)
		},
		krtOpts.ToOptions("ServiceEntryEndpoints")...,
	)
}

// endpointsFromWorkloads converts a group of SelectedWorkloads into
// ir.EndpointsForBackend in the context of a single ServiceEntry backend.
// We use selectedWorkload as the input because:
// 1. It lets us re-use code for both Pod and WorkloadEntry
// 2. We can easily construct synthetic selectedWorkload for inline endpoints on a ServiceEntry
func endpointsFromWorkloads(
	se *networkingclient.ServiceEntry,
	be ir.BackendObjectIR,
	workloads []selectedWorkload,
) *ir.EndpointsForBackend {
	if len(workloads) == 0 {
		return ir.NewEndpointsForBackend(be)
	}

	// this should never miss, we only call buildInlineCLA using BackendObjectIR
	// generated from the ServiceEntry iteself
	var servicePort *networking.ServicePort
	for _, sp := range se.Spec.GetPorts() {
		if int32(sp.GetNumber()) == be.Port {
			servicePort = sp
			break
		}
	}

	seTargetPort := be.Port
	if sePortTargetPort := servicePort.GetTargetPort(); sePortTargetPort > 0 {
		seTargetPort = int32(sePortTargetPort)
	}

	eps := ir.NewEndpointsForBackend(be)
	for _, workload := range workloads {
		address := workload.Address()
		if address == "" {
			continue
		}

		// for static, it must be an IP
		// for DNS it can be IP or hostname
		if se.Spec.GetResolution() == networking.ServiceEntry_STATIC {
			if net.ParseIP(address) == nil {
				continue
			}
		}

		// respect target ports on the WorkloadEntry
		epPort := workload.mapPort(servicePort.GetName(), seTargetPort)

		// only MESH_INTERNAL can use auto mTLS
		allowAutoMTLS := se.Spec.GetLocation() == networking.ServiceEntry_MESH_INTERNAL

		ep := ir.EndpointWithMd{
			LbEndpoint: krtcollections.CreateLBEndpoint(address, uint32(epPort), workload.AugmentedLabels, allowAutoMTLS),
			EndpointMd: ir.EndpointMetadata{
				Labels: workload.AugmentedLabels,
			},
		}
		if workload.weight > 0 {
			ep.LbEndpoint.LoadBalancingWeight = &wrapperspb.UInt32Value{Value: workload.weight}
		}
		eps.Add(workload.Locality, ep)
	}
	return eps
}
