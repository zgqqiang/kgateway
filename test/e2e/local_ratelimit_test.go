//go:build ignore

package e2e_test

import (
	"fmt"
	"net/http"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	v1 "github.com/kgateway-dev/kgateway/v2/internal/gateway/pkg/api/v1"
	gatewaydefaults "github.com/kgateway-dev/kgateway/v2/internal/gateway/pkg/defaults"
	gloov1 "github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/api/v1"
	gloo_matchers "github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/api/v1/core/matchers"
	"github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/api/v1/enterprise/options/ratelimit"
	"github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/api/v1/options/local_ratelimit"
	local_ratelimit_plugin "github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/plugins/local_ratelimit"
	"github.com/kgateway-dev/kgateway/v2/test/e2e"
	"github.com/kgateway-dev/kgateway/v2/test/gomega/matchers"
	"github.com/kgateway-dev/kgateway/v2/test/testutils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Local Rate Limit", func() {

	const (
		defaultLimit = 3
		vsLimit      = 2
		routeLimit   = 1
	)

	var (
		testContext *e2e.TestContext

		httpClient                                  *http.Client
		requestBuilder                              *testutils.HttpRequestBuilder
		expectNotRateLimitedWithOutXRateLimitHeader func()
		expectNotRateLimitedWithXRateLimitHeader    func()
		expectRateLimitedWithXRateLimitHeader       func(int)
		validateRateLimits                          func(int)
	)

	BeforeEach(func() {
		testContext = testContextFactory.NewTestContext()
		testContext.BeforeEach()

		httpClient = testutils.DefaultHttpClient
		requestBuilder = testContext.GetHttpRequestBuilder()

		expectNotRateLimited := func() *http.Response {
			response, err := httpClient.Do(requestBuilder.Build())
			ExpectWithOffset(1, response).To(matchers.HaveOkResponse())
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "The connection should not be rate limited")
			return response
		}

		expectNotRateLimitedWithOutXRateLimitHeader = func() {
			response := expectNotRateLimited()
			// Since the values of the x-rate-limit headers change with time, we only check the presence of these header keys and not match their value
			ExpectWithOffset(1, response).ToNot(matchers.ContainHeaderKeys([]string{"x-ratelimit-limit", "x-ratelimit-remaining"}),
				"x-ratelimit headers should not be present for non rate limited requests")
		}

		expectNotRateLimitedWithXRateLimitHeader = func() {
			response := expectNotRateLimited()
			ExpectWithOffset(2, response).To(matchers.ContainHeaderKeys([]string{"x-ratelimit-limit", "x-ratelimit-remaining"}),
				"x-ratelimit headers should be present")
		}

		expectRateLimitedWithXRateLimitHeader = func(limit int) {
			response, err := httpClient.Do(requestBuilder.Build())
			ExpectWithOffset(2, response).To(matchers.HaveHttpResponse(&matchers.HttpResponse{
				StatusCode: http.StatusTooManyRequests,
				Body:       "local_rate_limited",
			}), "should rate limit")
			// Since the request should be rate limited at this point, the values of the following x-rate-limit headers should be consistent
			ExpectWithOffset(1, response).To(matchers.ContainHeaders(http.Header{
				"x-ratelimit-limit":     []string{fmt.Sprint(limit)},
				"x-ratelimit-remaining": []string{"0"},
			}), "x-ratelimit headers should be present")
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "There should be no error when rate limited")
		}

		validateRateLimits = func(limit int) {
			for range limit {
				expectNotRateLimitedWithXRateLimitHeader()
			}
			expectRateLimitedWithXRateLimitHeader(limit)
		}
	})

	AfterEach(func() {
		testContext.AfterEach()
	})

	JustBeforeEach(func() {
		testContext.JustBeforeEach()
	})

	JustAfterEach(func() {
		testContext.JustAfterEach()
	})

	Context("Filter not configured", func() {
		It("Should not add the filter to the list of HCM filters", func() {
			// Since the value of RemoveUnusedFilters is set to true in e2e tests, and this filter is not configured in gloo,
			// The filter should not be present in the envoy config, and requests should not be rate limited
			cfg, err := testContext.EnvoyInstance().ConfigDump()
			Expect(err).NotTo(HaveOccurred())
			Expect(cfg).ToNot(ContainSubstring(local_ratelimit_plugin.NetworkFilterStatPrefix))
			Expect(cfg).ToNot(ContainSubstring(local_ratelimit_plugin.HTTPFilterStatPrefix))

			// Since the filter is not defined, the custom X-RateLimit headers should not be present
			expectNotRateLimitedWithOutXRateLimitHeader()
		})
	})

	Context("L4 Local Rate Limit", func() {
		BeforeEach(func() {
			gw := gatewaydefaults.DefaultGateway(writeNamespace)
			gw.GetHttpGateway().Options = &gloov1.HttpListenerOptions{
				NetworkLocalRatelimit: &local_ratelimit.TokenBucket{
					MaxTokens: 1,
					TokensPerFill: &wrapperspb.UInt32Value{
						Value: 1,
					},
					FillInterval: &durationpb.Duration{
						Seconds: 100,
					},
				},
			}

			testContext.ResourcesToCreate().Gateways = v1.GatewayList{
				gw,
			}
		})

		// This test has flaked before with the following envoy error :
		// [error][envoy_bug] [external/envoy/source/common/http/conn_manager_impl.cc:527] envoy bug failure: !local_close_reason.empty(). Details: Local Close Reason was not set!
		// This has been fixed in envoy v1.27.0 but since we still use v1.26.gitx, this issue intermittently occurs.
		// Since this bug doesn't affect the functionally of the ConnectionLimit filter (requests still do not cross the limit), we're adding FlakeAttempts until we move to a version of envoy with this fix.
		// More info can be found here : https://github.com/kgateway-dev/kgateway/issues/8531
		It("Should rate limit at the l4 layer", FlakeAttempts(5), func() {
			expectNotRateLimited := func() {
				// We use a new client every time as TCP connections are cached and this needs to be avoided in order to test L4 rate limiting
				httpClient := testutils.DefaultClientBuilder().Build()
				response, err := httpClient.Do(requestBuilder.Build())
				ExpectWithOffset(1, response).To(matchers.HaveOkResponse())
				ExpectWithOffset(1, err).NotTo(HaveOccurred(), "The connection should not be rate limited")
			}

			expectRateLimited := func() {
				// We use a new client every time as TCP connections are cached and this needs to be avoided in order to test L4 rate limiting
				httpClient := testutils.DefaultClientBuilder().Build()
				_, err := httpClient.Do(requestBuilder.Build())
				ExpectWithOffset(1, err).Should(MatchError(ContainSubstring("EOF")), "The connection be limited")
			}

			// The rate limit is 1
			expectNotRateLimited()
			expectRateLimited()
		})
	})

	Context("HTTP Local Rate Limit", func() {
		Context("With the gateway level default rate limit set", func() {
			BeforeEach(func() {
				gw := gatewaydefaults.DefaultGateway(writeNamespace)
				gw.GetHttpGateway().Options = &gloov1.HttpListenerOptions{
					HttpLocalRatelimit: &local_ratelimit.Settings{
						EnableXRatelimitHeaders: &wrapperspb.BoolValue{Value: true},
						DefaultLimit: &local_ratelimit.TokenBucket{
							MaxTokens: defaultLimit,
							TokensPerFill: &wrapperspb.UInt32Value{
								Value: defaultLimit,
							},
							FillInterval: &durationpb.Duration{
								Seconds: 100,
							},
						},
					},
				}

				testContext.ResourcesToCreate().Gateways = v1.GatewayList{
					gw,
				}
			})

			It("Should rate limit the default value config when nothing else overrides it", func() {
				validateRateLimits(defaultLimit)
			})

			It("Should override the default rate limit with the virtual service rate limit", func() {
				testContext.PatchDefaultVirtualService(func(vs *v1.VirtualService) *v1.VirtualService {
					vs.GetVirtualHost().Options = &gloov1.VirtualHostOptions{
						RateLimitConfigType: &gloov1.VirtualHostOptions_Ratelimit{
							Ratelimit: &ratelimit.RateLimitVhostExtension{
								LocalRatelimit: &local_ratelimit.TokenBucket{
									MaxTokens: vsLimit,
									TokensPerFill: &wrapperspb.UInt32Value{
										Value: vsLimit,
									},
									FillInterval: &durationpb.Duration{
										Seconds: 100,
									},
								},
							},
						},
					}
					return vs
				})

				Eventually(func(g Gomega) {
					cfg, err := testContext.EnvoyInstance().ConfigDump()
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(cfg).To(ContainSubstring("typed_per_filter_config"))
				}, "5s", ".5s").Should(Succeed())

				validateRateLimits(vsLimit)
			})

			It("Should override the default rate limit with the route level rate limit", func() {
				testContext.PatchDefaultVirtualService(func(vs *v1.VirtualService) *v1.VirtualService {
					vs.GetVirtualHost().Options = &gloov1.VirtualHostOptions{
						RateLimitConfigType: &gloov1.VirtualHostOptions_Ratelimit{
							Ratelimit: &ratelimit.RateLimitVhostExtension{
								LocalRatelimit: &local_ratelimit.TokenBucket{
									MaxTokens: vsLimit,
									TokensPerFill: &wrapperspb.UInt32Value{
										Value: vsLimit,
									},
									FillInterval: &durationpb.Duration{
										Seconds: 100,
									},
								},
							},
						},
					}
					vs.GetVirtualHost().GetRoutes()[0].Options = &gloov1.RouteOptions{
						RateLimitConfigType: &gloov1.RouteOptions_Ratelimit{
							Ratelimit: &ratelimit.RateLimitRouteExtension{
								LocalRatelimit: &local_ratelimit.TokenBucket{
									MaxTokens: routeLimit,
									TokensPerFill: &wrapperspb.UInt32Value{
										Value: routeLimit,
									},
									FillInterval: &durationpb.Duration{
										Seconds: 100,
									},
								},
							},
						},
					}
					return vs
				})

				Eventually(func(g Gomega) {
					cfg, err := testContext.EnvoyInstance().ConfigDump()
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(cfg).To(ContainSubstring("typed_per_filter_config"))
				}, "5s", ".5s").Should(Succeed())

				validateRateLimits(routeLimit)
			})

			Context("No gateway level default rate limit set", func() {
				BeforeEach(func() {
					gw := gatewaydefaults.DefaultGateway(writeNamespace)
					gw.GetHttpGateway().Options = &gloov1.HttpListenerOptions{
						HttpLocalRatelimit: &local_ratelimit.Settings{
							EnableXRatelimitHeaders: &wrapperspb.BoolValue{Value: true},
						},
					}

					testContext.ResourcesToCreate().Gateways = v1.GatewayList{
						gw,
					}
				})

				It("Should not rate limit if there is no gateway, vhost or route limit specified", func() {
					// If the default is not specified and neither the vHost or Route are RL, the filter should not be applied
					cfg, err := testContext.EnvoyInstance().ConfigDump()
					Expect(err).NotTo(HaveOccurred())
					Expect(cfg).ToNot(ContainSubstring(local_ratelimit_plugin.HTTPFilterStatPrefix))

					// Since the filter defined, but not configured with any limits, the X-RateLimit headers should not be present
					expectNotRateLimitedWithOutXRateLimitHeader()
				})

				It("Should rate limit only the route that has an rate limit specified", func() {
					testContext.PatchDefaultVirtualService(func(vs *v1.VirtualService) *v1.VirtualService {
						routes := vs.GetVirtualHost().GetRoutes()
						routes[0].Options = &gloov1.RouteOptions{
							RateLimitConfigType: &gloov1.RouteOptions_Ratelimit{
								Ratelimit: &ratelimit.RateLimitRouteExtension{
									LocalRatelimit: &local_ratelimit.TokenBucket{
										MaxTokens: routeLimit,
										TokensPerFill: &wrapperspb.UInt32Value{
											Value: routeLimit,
										},
										FillInterval: &durationpb.Duration{
											Seconds: 100,
										},
									},
								},
							},
						}
						unlimitedRoute := &v1.Route{
							Matchers: []*gloo_matchers.Matcher{
								{
									PathSpecifier: &gloo_matchers.Matcher_Prefix{
										Prefix: "/unlimited",
									},
								},
							},
							Action: &v1.Route_DirectResponseAction{
								DirectResponseAction: &gloov1.DirectResponseAction{
									Status: 200,
									Body:   "unlimited",
								},
							},
						}
						routes = append([]*v1.Route{
							unlimitedRoute,
						}, routes...)
						vs.VirtualHost.Routes = routes
						return vs
					})

					// The default is not specified and only the Route is RL
					Eventually(func(g Gomega) {
						cfg, err := testContext.EnvoyInstance().ConfigDump()
						g.Expect(err).NotTo(HaveOccurred())
						g.Expect(cfg).To(ContainSubstring("typed_per_filter_config"))
					}, "5s", ".5s").Should(Succeed())

					validateRateLimits(routeLimit)

					// Since the filter is not configured on the /unlimited path, the X-RateLimit headers should not be present
					requestBuilder = requestBuilder.WithPath("unlimited")
					expectNotRateLimitedWithOutXRateLimitHeader()
				})

			})
		})
	})
})
