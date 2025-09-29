package common

import (
	"context"

	"github.com/go-logr/logr"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube"
	istiokube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/kubetypes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	extensionsplug "github.com/kgateway-dev/kgateway/v2/internal/kgateway/extensions2/plugin"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/extensions2/settings"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/ir"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/krtcollections"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/utils/krtutil"
	"github.com/kgateway-dev/kgateway/v2/pkg/client/clientset/versioned"

	networkingclient "istio.io/client-go/pkg/apis/networking/v1"
)

type CommonCollections struct {
	OurClient versioned.Interface
	Client    kube.Client
	// full CRUD client, only needed for status writing currently
	CrudClient        client.Client
	KrtOpts           krtutil.KrtOptions
	Secrets           *krtcollections.SecretIndex
	BackendIndex      *krtcollections.BackendIndex
	Routes            *krtcollections.RoutesIndex
	Namespaces        krt.Collection[krtcollections.NamespaceMetadata]
	Endpoints         krt.Collection[ir.EndpointsForBackend]
	GatewayIndex      *krtcollections.GatewayIndex
	GatewayExtensions krt.Collection[ir.GatewayExtension]
	Services          krt.Collection[*corev1.Service]
	ServiceEntries    krt.Collection[*networkingclient.ServiceEntry]

	Pods       krt.Collection[krtcollections.LocalityPod]
	RefGrants  *krtcollections.RefGrantIndex
	ConfigMaps krt.Collection[*corev1.ConfigMap]

	// static set of global Settings, non-krt based for dev speed
	// TODO: this should be refactored to a more correct location,
	// or even better, be removed entirely and done per Gateway (maybe in GwParams)
	Settings       settings.Settings
	controllerName string
}

func (c *CommonCollections) HasSynced() bool {
	// we check nil as well because some of the inner
	// collections aren't initialized until we call InitPlugins
	return c.Secrets != nil && c.Secrets.HasSynced() &&
		c.BackendIndex != nil && c.BackendIndex.HasSynced() &&
		c.Routes != nil && c.Routes.HasSynced() &&
		c.Namespaces != nil && c.Namespaces.HasSynced() &&
		c.Pods != nil && c.Pods.HasSynced() &&
		c.RefGrants != nil && c.RefGrants.HasSynced() &&
		c.ConfigMaps != nil && c.ConfigMaps.HasSynced() &&
		c.GatewayExtensions != nil && c.GatewayExtensions.HasSynced()
	// TODO: fill in?
}

// NewCommonCollections initializes the core krt collections.
// Collections that rely on plugins aren't initialized here,
// and InitPlugins must be called.
func NewCommonCollections(
	ctx context.Context,
	krtOptions krtutil.KrtOptions,
	client istiokube.Client,
	ourClient versioned.Interface,
	cl client.Client,
	controllerName string,
	logger logr.Logger,
	settings settings.Settings,
) *CommonCollections {
	secretClient := kclient.New[*corev1.Secret](client)
	k8sSecretsRaw := krt.WrapClient(secretClient, krt.WithStop(krtOptions.Stop), krt.WithName("Secrets") /* no debug here - we don't want raw secrets printed*/)
	k8sSecrets := krt.NewCollection(k8sSecretsRaw, func(kctx krt.HandlerContext, i *corev1.Secret) *ir.Secret {
		res := ir.Secret{
			ObjectSource: ir.ObjectSource{
				Group:     "",
				Kind:      "Secret",
				Namespace: i.Namespace,
				Name:      i.Name,
			},
			Obj:  i,
			Data: i.Data,
		}
		return &res
	}, krtOptions.ToOptions("secrets")...)
	secrets := map[schema.GroupKind]krt.Collection[ir.Secret]{
		{Group: "", Kind: "Secret"}: k8sSecrets,
	}

	refgrantsCol := krt.WrapClient(kclient.New[*gwv1beta1.ReferenceGrant](client), krtOptions.ToOptions("RefGrants")...)
	refgrants := krtcollections.NewRefGrantIndex(refgrantsCol)

	namespaces := krtcollections.NewNamespaceCollection(ctx, client, krtOptions)

	serviceClient := kclient.New[*corev1.Service](client)
	services := krt.WrapClient(serviceClient, krtOptions.ToOptions("Services")...)

	seInformer := kclient.NewDelayedInformer[*networkingclient.ServiceEntry](
		client, gvr.ServiceEntry,
		kubetypes.StandardInformer, kclient.Filter{ObjectFilter: client.ObjectFilter()},
	)
	serviceEntries := krt.WrapClient(seInformer, krtOptions.ToOptions("ServiceEntries")...)

	cmClient := kclient.New[*corev1.ConfigMap](client)
	cfgmaps := krt.WrapClient(cmClient, krtOptions.ToOptions("ConfigMaps")...)

	gwExts := krtcollections.NewGatewayExtensionsCollection(ctx, client, ourClient, krtOptions)

	return &CommonCollections{
		OurClient:         ourClient,
		Client:            client,
		CrudClient:        cl,
		KrtOpts:           krtOptions,
		Secrets:           krtcollections.NewSecretIndex(secrets, refgrants),
		Pods:              krtcollections.NewPodsCollection(client, krtOptions),
		RefGrants:         refgrants,
		Settings:          settings,
		Namespaces:        namespaces,
		Services:          services,
		ServiceEntries:    serviceEntries,
		ConfigMaps:        cfgmaps,
		GatewayExtensions: gwExts,

		controllerName: controllerName,
	}
}

// InitPlugins set up collections that rely on plugins.
// This can't be part of NewCommonCollections because the setup
// of plugins themselves rely on a reference to CommonCollections.
func (c *CommonCollections) InitPlugins(ctx context.Context, mergedPlugins extensionsplug.Plugin) {
	gateways, routeIndex, backendIndex, endpointIRs := krtcollections.InitCollections(
		ctx,
		c.controllerName,
		mergedPlugins,
		c.Client,
		c.OurClient,
		c.RefGrants,
		c.KrtOpts,
	)

	// init plugin-extended collections
	c.BackendIndex = backendIndex
	c.Routes = routeIndex
	c.Endpoints = endpointIRs
	c.GatewayIndex = gateways
}
