//go:build ignore

package gloo_test

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/kgateway-dev/kgateway/v2/pkg/utils/kubeutils/portforward"

	"github.com/kgateway-dev/kgateway/v2/pkg/utils/settingsutil"

	"github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/bootstrap"
	"github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/syncer/setup"
	"github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/xds"
	"github.com/kgateway-dev/kgateway/v2/pkg/bootstrap/leaderelector"
	"github.com/kgateway-dev/kgateway/v2/pkg/bootstrap/leaderelector/singlereplica"
	"github.com/kgateway-dev/kgateway/v2/pkg/utils/setuputils"

	"github.com/solo-io/solo-kit/pkg/api/v1/clients"

	"github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/defaults"

	"github.com/golang/protobuf/ptypes/wrappers"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/solo-io/solo-kit/pkg/api/v1/clients/kube"
	"github.com/solo-io/solo-kit/pkg/api/v1/clients/memory"
	"github.com/solo-io/solo-kit/pkg/utils/prototime"
	"google.golang.org/grpc"

	"github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/api/grpc/validation"
	v1 "github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/api/v1"
	"github.com/kgateway-dev/kgateway/v2/test/helpers"
	"github.com/kgateway-dev/kgateway/v2/test/kube2e"
)

var _ = Describe("Setup Syncer", func() {

	var (
		setupLock sync.RWMutex
	)

	// SetupFunc is used to configure Gloo with appropriate configuration
	// It is assumed to run once at construction time, and therefore it executes directives that
	// are also assumed to only run at construction time.
	// One of those, is the construction of schemes: https://github.com/kubernetes/kubernetes/pull/89019#issuecomment-600278461
	// In our tests we do not follow this pattern, and to avoid data races (that cause test failures)
	// we ensure that only 1 SetupFunc is ever called at a time
	newSynchronizedSetupFunc := func() setuputils.SetupFunc {
		setupOpts := bootstrap.NewSetupOpts(xds.NewAdsSnapshotCache(ctx), nil)
		setupFunc := setup.NewSetupFunc(setupOpts)

		var synchronizedSetupFunc setuputils.SetupFunc
		synchronizedSetupFunc = func(setupCtx context.Context, kubeCache kube.SharedCache, inMemoryCache memory.InMemoryResourceCache, settings *v1.Settings, identity leaderelector.Identity) error {
			setupLock.Lock()
			defer setupLock.Unlock()
			// This is normally performed within the SetupSyncer and is required by Gloo components
			setupCtx = settingsutil.WithSettings(setupCtx, settings)

			return setupFunc(setupCtx, kubeCache, inMemoryCache, settings, identity)
		}

		return synchronizedSetupFunc
	}

	Context("Kube Tests", func() {

		var (
			kubeCoreCache kube.SharedCache
			memCache      memory.InMemoryResourceCache

			settings *v1.Settings
		)

		BeforeEach(func() {
			var err error
			settings, err = resourceClientset.SettingsClient().Read(testHelper.InstallNamespace, defaults.SettingsName, clients.ReadOpts{Ctx: ctx})
			Expect(err).NotTo(HaveOccurred())

			settings.Gateway.Validation = nil
			settings.Gloo = &v1.GlooOptions{
				XdsBindAddr:        getRandomAddr(),
				ValidationBindAddr: getRandomAddr(),
				ProxyDebugBindAddr: getRandomAddr(),
			}

			kubeCoreCache = kube.NewKubeCache(ctx)
			memCache = memory.NewInMemoryResourceCache()
		})

		It("can be called with core cache", func() {
			setup := newSynchronizedSetupFunc()
			err := setup(ctx, kubeCoreCache, memCache, settings, singlereplica.Identity())
			Expect(err).NotTo(HaveOccurred())
		})

		It("can be called with core cache warming endpoints", func() {
			settings.Gloo.EndpointsWarmingTimeout = prototime.DurationToProto(time.Minute)
			setup := newSynchronizedSetupFunc()
			err := setup(ctx, kubeCoreCache, memCache, settings, singlereplica.Identity())
			Expect(err).NotTo(HaveOccurred())
		})

		It("panics when endpoints don't arrive in a timely manner", func() {
			settings.Gloo.EndpointsWarmingTimeout = prototime.DurationToProto(1 * time.Nanosecond)
			setup := newSynchronizedSetupFunc()
			Expect(func() {
				_ = setup(ctx, kubeCoreCache, memCache, settings, singlereplica.Identity())
			}).To(Panic())
		})

		It("doesn't panic when endpoints don't arrive in a timely manner if set to zero", func() {
			settings.Gloo.EndpointsWarmingTimeout = prototime.DurationToProto(0)
			setup := newSynchronizedSetupFunc()
			Expect(func() {
				_ = setup(ctx, kubeCoreCache, memCache, settings, singlereplica.Identity())
			}).NotTo(Panic())
		})

		It("restarts validation grpc server when settings change", func() {
			portForwarder, err := testHelper.StartPortForward(ctx,
				portforward.WithDeployment(helpers.DefaultKgatewayDeploymentName, testHelper.InstallNamespace),
				portforward.WithRemotePort(defaults.GlooValidationPort),
			)
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				portForwarder.Close()
				portForwarder.WaitForStop()
			}()

			cc, err := grpc.DialContext(ctx, portForwarder.Address(), grpc.WithInsecure())
			Expect(err).NotTo(HaveOccurred())
			validationClient := validation.NewGlooValidationServiceClient(cc)
			validationRequest := &validation.GlooValidationServiceRequest{
				Proxy: &v1.Proxy{
					Listeners: []*v1.Listener{
						{Name: "test-listener"},
					},
				},
			}

			Eventually(func(g Gomega) {
				_, err := validationClient.Validate(ctx, validationRequest)
				g.Expect(err).NotTo(HaveOccurred())
			}, "10s", "1s").Should(Succeed(), "validation request should succeed")

			kube2e.UpdateSettings(ctx, func(settings *v1.Settings) {
				settings.Gateway.Validation.ValidationServerGrpcMaxSizeBytes = &wrappers.Int32Value{Value: 1}
			}, testHelper.InstallNamespace)

			Eventually(func(g Gomega) {
				_, err := validationClient.Validate(ctx, validationRequest)
				g.Expect(err).To(MatchError(ContainSubstring("received message larger than max (19 vs. 1)")))
			}, "10s", "1s").Should(Succeed(), "validation request should fail")
		})
	})
})

func getRandomAddr() string {
	listener, err := net.Listen("tcp", "localhost:0")
	Expect(err).NotTo(HaveOccurred())
	addr := listener.Addr().String()
	err = listener.Close()
	Expect(err).NotTo(HaveOccurred())
	return addr
}
