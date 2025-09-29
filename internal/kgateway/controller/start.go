package controller

import (
	"context"

	envoycache "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	"github.com/solo-io/go-utils/contextutils"
	uzap "go.uber.org/zap"
	istiokube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
	istiolog "istio.io/istio/pkg/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	czap "sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	infextv1a2 "sigs.k8s.io/gateway-api-inference-extension/api/v1alpha2"

	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/deployer"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/extensions2"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/extensions2/common"
	extensionsplug "github.com/kgateway-dev/kgateway/v2/internal/kgateway/extensions2/plugin"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/extensions2/plugins/inferenceextension/endpointpicker"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/extensions2/registry"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/extensions2/settings"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/ir"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/krtcollections"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/proxy_syncer"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/utils/krtutil"
	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/wellknown"
	"github.com/kgateway-dev/kgateway/v2/pkg/client/clientset/versioned"
	kgtwschemes "github.com/kgateway-dev/kgateway/v2/pkg/schemes"
	"github.com/kgateway-dev/kgateway/v2/pkg/utils/kubeutils"
	"github.com/kgateway-dev/kgateway/v2/pkg/utils/namespaces"
)

const (
	// AutoProvision controls whether the controller will be responsible for provisioning dynamic
	// infrastructure for the Gateway API.
	AutoProvision = true
)

type SetupOpts struct {
	Cache envoycache.SnapshotCache

	KrtDebugger *krt.DebugHandler

	// static set of global Settings
	GlobalSettings *settings.Settings

	PprofBindAddress       string
	HealthProbeBindAddress string
	MetricsBindAddress     string
}

var setupLog = ctrl.Log.WithName("setup")

type StartConfig struct {
	ControllerName string

	Dev        bool
	SetupOpts  *SetupOpts
	RestConfig *rest.Config
	// ExtensionsFactory is the factory function which will return an extensions.K8sGatewayExtensions
	// This is responsible for producing the extension points that this controller requires
	ExtraPlugins func(ctx context.Context, commoncol *common.CommonCollections) []extensionsplug.Plugin

	Client istiokube.Client

	AugmentedPods krt.Collection[krtcollections.LocalityPod]
	UniqueClients krt.Collection[ir.UniqlyConnectedClient]

	KrtOptions krtutil.KrtOptions
}

// Start runs the controllers responsible for processing the K8s Gateway API objects
// It is intended to be run in a goroutine as the function will block until the supplied
// context is cancelled
type ControllerBuilder struct {
	proxySyncer *proxy_syncer.ProxySyncer
	cfg         StartConfig
	mgr         ctrl.Manager
}

func NewControllerBuilder(ctx context.Context, cfg StartConfig) (*ControllerBuilder, error) {
	var opts []czap.Opts
	loggingOptions := istiolog.DefaultOptions()

	if cfg.Dev {
		setupLog.Info("starting log in dev mode")
		opts = append(opts, czap.UseDevMode(true))
		loggingOptions.SetDefaultOutputLevel(istiolog.OverrideScopeName, istiolog.DebugLevel)
	}
	ctrl.SetLogger(czap.New(opts...))
	istiolog.Configure(loggingOptions)

	scheme := DefaultScheme()

	// Extend the scheme if the TCPRoute CRD exists.
	if err := kgtwschemes.AddGatewayV1A2Scheme(cfg.RestConfig, scheme); err != nil {
		return nil, err
	}

	mgrOpts := ctrl.Options{
		BaseContext:      func() context.Context { return ctx },
		Scheme:           scheme,
		PprofBindAddress: cfg.SetupOpts.PprofBindAddress,
		// if you change the port here, also change the port "health" in the helmchart.
		HealthProbeBindAddress: cfg.SetupOpts.HealthProbeBindAddress,
		Metrics: metricsserver.Options{
			BindAddress: cfg.SetupOpts.MetricsBindAddress,
		},
		Controller: config.Controller{
			// see https://github.com/kubernetes-sigs/controller-runtime/issues/2937
			// in short, our tests reuse the same name (reasonably so) and the controller-runtime
			// package does not reset the stack of controller names between tests, so we disable
			// the name validation here.
			SkipNameValidation: ptr.To(true),
		},
	}
	mgr, err := ctrl.NewManager(cfg.RestConfig, mgrOpts)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return nil, err
	}

	// TODO: replace this with something that checks that we have xds snapshot ready (or that we don't need one).
	mgr.AddReadyzCheck("ready-ping", healthz.Ping)

	setupLog.Info("initializing kgateway extensions")
	// Extend the scheme and add the EPP plugin if the inference extension is enabled and the InferencePool CRD exists.
	if cfg.SetupOpts.GlobalSettings.EnableInferExt {
		exists, err := kgtwschemes.AddInferExtV1A2Scheme(cfg.RestConfig, scheme)
		switch {
		case err != nil:
			return nil, err
		case exists:
			setupLog.Info("adding endpoint-picker inference extension")

			existingExtraPlugins := cfg.ExtraPlugins
			cfg.ExtraPlugins = func(ctx context.Context, commoncol *common.CommonCollections) []extensionsplug.Plugin {
				var plugins []extensionsplug.Plugin

				// Add the inference extension plugin.
				if plug := endpointpicker.NewPlugin(ctx, commoncol); plug != nil {
					plugins = append(plugins, *plug)
				}

				// If there was an existing ExtraPlugins function, append its plugins too.
				if existingExtraPlugins != nil {
					plugins = append(plugins, existingExtraPlugins(ctx, commoncol)...)
				}

				return plugins
			}
		}
	}

	cli, err := versioned.NewForConfig(cfg.RestConfig)
	if err != nil {
		return nil, err
	}
	commoncol := common.NewCommonCollections(
		ctx,
		cfg.KrtOptions,
		cfg.Client,
		cli,
		mgr.GetClient(),
		cfg.ControllerName,
		setupLog,
		*cfg.SetupOpts.GlobalSettings,
	)
	mergedPlugins := pluginFactoryWithBuiltin(cfg.ExtraPlugins)(ctx, commoncol)
	commoncol.InitPlugins(ctx, mergedPlugins)

	// Create the proxy syncer for the Gateway API resources
	setupLog.Info("initializing proxy syncer")
	proxySyncer := proxy_syncer.NewProxySyncer(
		ctx,
		cfg.ControllerName,
		mgr,
		cfg.Client,
		cfg.UniqueClients,
		mergedPlugins,
		commoncol,
		cfg.SetupOpts.Cache,
	)
	proxySyncer.Init(ctx, cfg.KrtOptions)

	if err := mgr.Add(proxySyncer); err != nil {
		setupLog.Error(err, "unable to add proxySyncer runnable")
		return nil, err
	}

	setupLog.Info("starting controller builder")
	return &ControllerBuilder{
		proxySyncer: proxySyncer,
		cfg:         cfg,
		mgr:         mgr,
	}, nil
}

func pluginFactoryWithBuiltin(extraPlugins func(ctx context.Context, commoncol *common.CommonCollections) []extensionsplug.Plugin) extensions2.K8sGatewayExtensionsFactory {
	return func(ctx context.Context, commoncol *common.CommonCollections) extensionsplug.Plugin {
		plugins := registry.Plugins(ctx, commoncol)
		plugins = append(plugins, krtcollections.NewBuiltinPlugin(ctx))
		if extraPlugins != nil {
			plugins = append(plugins, extraPlugins(ctx, commoncol)...)
		}
		return registry.MergePlugins(plugins...)
	}
}

func (c *ControllerBuilder) Start(ctx context.Context) error {
	logger := contextutils.LoggerFrom(ctx).Desugar()
	logger.Info("starting gateway controller")

	globalSettings := c.cfg.SetupOpts.GlobalSettings

	xdsHost := kubeutils.ServiceFQDN(metav1.ObjectMeta{
		Name:      globalSettings.XdsServiceName,
		Namespace: namespaces.GetPodNamespace(),
	})
	xdsPort := globalSettings.XdsServicePort
	logger.Info("got xds address for deployer", uzap.String("xds_host", xdsHost), uzap.Uint32("xds_port", xdsPort))

	istioAutoMtlsEnabled := globalSettings.EnableIstioAutoMtls

	gwCfg := GatewayConfig{
		Mgr:            c.mgr,
		ControllerName: c.cfg.ControllerName,
		AutoProvision:  AutoProvision,
		ControlPlane: deployer.ControlPlaneInfo{
			XdsHost: xdsHost,
			XdsPort: xdsPort,
		},
		IstioAutoMtlsEnabled: istioAutoMtlsEnabled,
		ImageInfo: &deployer.ImageInfo{
			Registry:   globalSettings.DefaultImageRegistry,
			Tag:        globalSettings.DefaultImageTag,
			PullPolicy: globalSettings.DefaultImagePullPolicy,
		},
	}

	setupLog.Info("creating gateway class provisioner")
	if err := NewGatewayClassProvisioner(c.mgr, c.cfg.ControllerName, GetDefaultClassInfo()); err != nil {
		setupLog.Error(err, "unable to create gateway class provisioner")
		return err
	}

	setupLog.Info("creating base gateway controller")
	if err := NewBaseGatewayController(ctx, gwCfg); err != nil {
		setupLog.Error(err, "unable to create gateway controller")
		return err
	}

	setupLog.Info("creating inferencepool controller")
	// Create the InferencePool controller if the inference extension feature is enabled and the API group is registered.
	if globalSettings.EnableInferExt &&
		c.mgr.GetScheme().IsGroupRegistered(infextv1a2.GroupVersion.Group) {
		poolCfg := &InferencePoolConfig{
			Mgr: c.mgr,
			// TODO read this from globalSettings
			ControllerName: c.cfg.ControllerName,
		}
		// Enable the inference extension deployer if set.
		if globalSettings.InferExtAutoProvision {
			poolCfg.InferenceExt = new(deployer.InferenceExtInfo)
		}
		if err := NewBaseInferencePoolController(ctx, poolCfg, &gwCfg); err != nil {
			setupLog.Error(err, "unable to create inferencepool controller")
			return err
		}
	}

	setupLog.Info("starting manager")
	return c.mgr.Start(ctx)
}

// GetDefaultClassInfo returns the default GatewayClass for the kgateway controller.
// Exported for testing.
func GetDefaultClassInfo() map[string]*ClassInfo {
	return map[string]*ClassInfo{
		wellknown.GatewayClassName: {
			Description: "Standard class for managing Gateway API ingress traffic.",
			Labels:      map[string]string{},
			Annotations: map[string]string{},
		},
		wellknown.WaypointClassName: {
			Description: "Specialized class for Istio ambient mesh waypoint proxies.",
			Labels:      map[string]string{},
			Annotations: map[string]string{
				"ambient.istio.io/waypoint-inbound-binding": "PROXY/15088",
			},
		},
	}
}
