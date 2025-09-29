package proxy_syncer_test

import (
	"testing"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	endpointv3 "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/endpoints"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/ir"
)

func TestTranslatesDestrulesFailoverPriority(t *testing.T) {
	g := gomega.NewWithT(t)
	us := ir.BackendObjectIR{
		ObjectSource: ir.ObjectSource{
			Namespace: "ns",
			Name:      "name",
		},
	}
	efu := ir.NewEndpointsForBackend(us)
	efu.Add(ir.PodLocality{Region: "R1"}, ir.EndpointWithMd{
		LbEndpoint: &endpointv3.LbEndpoint{
			HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
				Endpoint: &endpointv3.Endpoint{
					Address: &corev3.Address{
						Address: &corev3.Address_Pipe{Pipe: &corev3.Pipe{Path: "a"}},
					},
				},
			},
		},
		EndpointMd: ir.EndpointMetadata{
			Labels: map[string]string{corev1.LabelTopologyRegion: "R1"},
		},
	})
	efu.Add(ir.PodLocality{Region: "R2"}, ir.EndpointWithMd{
		LbEndpoint: &endpointv3.LbEndpoint{
			HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
				Endpoint: &endpointv3.Endpoint{
					Address: &corev3.Address{
						Address: &corev3.Address_Pipe{Pipe: &corev3.Pipe{Path: "b"}},
					},
				},
			},
		},
		EndpointMd: ir.EndpointMetadata{
			Labels: map[string]string{corev1.LabelTopologyRegion: "R2"},
		},
	})
	ucc := ir.UniqlyConnectedClient{
		Namespace: "ns",
		Locality:  ir.PodLocality{Region: "R1"},
		Labels:    map[string]string{corev1.LabelTopologyRegion: "R1"},
	}

	priorityInfo := &endpoints.PriorityInfo{
		FailoverPriority: endpoints.NewPriorities([]string{
			"topology.kubernetes.io/region",
		}),
	}

	cla := endpoints.PrioritizeEndpoints(nil, priorityInfo, *efu, ucc)
	g.Expect(cla.Endpoints).To(gomega.HaveLen(2))

	remoteLocality := cla.Endpoints[0]
	localLocality := cla.Endpoints[1]
	if remoteLocality.Locality.Region == "R1" {
		remoteLocality = cla.Endpoints[1]
		localLocality = cla.Endpoints[0]
	}
	g.Expect(localLocality.Locality.Region).To(gomega.Equal("R1"))
	g.Expect(remoteLocality.Locality.Region).To(gomega.Equal("R2"))

	g.Expect(localLocality.Priority).To(gomega.Equal(uint32(0)))
	g.Expect(remoteLocality.Priority).To(gomega.Equal(uint32(1)))
}

// similar to TestTranslatesDestrulesFailoverPriority but implicit
func TestTranslatesDestrulesFailover(t *testing.T) {
	g := gomega.NewWithT(t)
	us := ir.BackendObjectIR{
		ObjectSource: ir.ObjectSource{
			Namespace: "ns",
			Name:      "name",
		},
	}
	efu := ir.NewEndpointsForBackend(us)
	efu.Add(ir.PodLocality{Region: "R1"}, ir.EndpointWithMd{
		LbEndpoint: &endpointv3.LbEndpoint{
			HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
				Endpoint: &endpointv3.Endpoint{
					Address: &corev3.Address{
						Address: &corev3.Address_Pipe{Pipe: &corev3.Pipe{Path: "a"}},
					},
				},
			},
		},
		EndpointMd: ir.EndpointMetadata{
			Labels: map[string]string{corev1.LabelTopologyRegion: "R1"},
		},
	})
	efu.Add(ir.PodLocality{Region: "R2"}, ir.EndpointWithMd{
		LbEndpoint: &endpointv3.LbEndpoint{
			HostIdentifier: &endpointv3.LbEndpoint_Endpoint{
				Endpoint: &endpointv3.Endpoint{
					Address: &corev3.Address{
						Address: &corev3.Address_Pipe{Pipe: &corev3.Pipe{Path: "b"}},
					},
				},
			},
		},
		EndpointMd: ir.EndpointMetadata{
			Labels: map[string]string{corev1.LabelTopologyRegion: "R2"},
		},
	})
	ucc := ir.UniqlyConnectedClient{
		Namespace: "ns",
		Locality:  ir.PodLocality{Region: "R1"},
		Labels:    map[string]string{corev1.LabelTopologyRegion: "R1"},
	}

	priorityInfo := &endpoints.PriorityInfo{}

	cla := endpoints.PrioritizeEndpoints(nil, priorityInfo, *efu, ucc)
	g.Expect(cla.Endpoints).To(gomega.HaveLen(2))

	remoteLocality := cla.Endpoints[0]
	localLocality := cla.Endpoints[1]
	if remoteLocality.Locality.Region == "R1" {
		remoteLocality = cla.Endpoints[1]
		localLocality = cla.Endpoints[0]
	}
	g.Expect(localLocality.Locality.Region).To(gomega.Equal("R1"))
	g.Expect(remoteLocality.Locality.Region).To(gomega.Equal("R2"))

	g.Expect(localLocality.Priority).To(gomega.Equal(uint32(0)))
	g.Expect(remoteLocality.Priority).To(gomega.Equal(uint32(1)))
}
