package krtcollections

import (
	"context"
	"maps"

	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"

	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/utils/krtutil"
)

type NamespaceMetadata struct {
	Name   string
	Labels map[string]string
}

func (n NamespaceMetadata) ResourceName() string {
	return n.Name
}

func (n NamespaceMetadata) Equals(in NamespaceMetadata) bool {
	return n.Name == in.Name && maps.Equal(n.Labels, in.Labels)
}

func NewNamespaceCollection(ctx context.Context, istioClient kube.Client, krtOpts krtutil.KrtOptions) krt.Collection[NamespaceMetadata] {
	client := kclient.NewFiltered[*corev1.Namespace](istioClient, kclient.Filter{
		// ObjectTransform: ...,
	})
	col := krt.WrapClient(client, krtOpts.ToOptions("Namespaces")...)
	return NewNamespaceCollectionFromCol(ctx, col, krtOpts)
}

func NewNamespaceCollectionFromCol(ctx context.Context, col krt.Collection[*corev1.Namespace], krtOpts krtutil.KrtOptions) krt.Collection[NamespaceMetadata] {
	return krt.NewCollection(col, func(ctx krt.HandlerContext, ns *corev1.Namespace) *NamespaceMetadata {
		return &NamespaceMetadata{
			Name:   ns.Name,
			Labels: ns.Labels,
		}
	}, krtOpts.ToOptions("NamespacesMetadata")...)
}
