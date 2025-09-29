//go:build ignore

package e2e_test

import (
	"encoding/json"
	"net/http"

	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/kgateway-dev/kgateway/v2/test/testutils"

	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/onsi/gomega/gstruct"

	v1 "github.com/kgateway-dev/kgateway/v2/internal/gateway/pkg/api/v1"
	"github.com/kgateway-dev/kgateway/v2/test/e2e"
	testmatchers "github.com/kgateway-dev/kgateway/v2/test/gomega/matchers"
	"github.com/kgateway-dev/kgateway/v2/test/gomega/transforms"
	"github.com/kgateway-dev/kgateway/v2/test/helpers"
	"github.com/kgateway-dev/kgateway/v2/test/v1helpers"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	gloov1 "github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/api/v1"
	"github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/api/v1/options/transformation"
)

var _ = Describe("Transformations", func() {

	var (
		testContext *e2e.TestContext
	)

	BeforeEach(func() {
		testContext = testContextFactory.NewTestContext()
		testContext.BeforeEach()
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

	Context("Parsing valid json", func() {

		var transform *transformation.Transformations

		BeforeEach(func() {
			transform = &transformation.Transformations{
				ResponseTransformation: &transformation.Transformation{
					TransformationType: &transformation.Transformation_TransformationTemplate{
						TransformationTemplate: &transformation.TransformationTemplate{
							BodyTransformation: &transformation.TransformationTemplate_Body{
								Body: &transformation.InjaTemplate{
									Text: "{{body}}",
								},
							},
							Headers: map[string]*transformation.InjaTemplate{
								"content-type": {
									Text: "text/html",
								},
							},
						},
					},
				},
			}
		})

		defaultPostBody := `{"body":"test"}`
		defaultOutput := "test"

		// EventuallyResponseTransformed returns an Asynchronous Assertion which
		// validates that a request with a body will return the requested content.
		// This will only work if a transformation is applied to the response
		EventuallyResponseTransformed := func(postBody, expectedOutput string) AsyncAssertion {
			requestBuilder := testContext.GetHttpRequestBuilder().WithPostBody(postBody)
			return Eventually(func(g Gomega) {
				g.Expect(testutils.DefaultHttpClient.Do(requestBuilder.Build())).To(testmatchers.HaveExactResponseBody(expectedOutput))
			}, "5s", ".5s")
		}

		It("should fail if no transform defined", func() {
			testContext.PatchDefaultVirtualService(func(vs *v1.VirtualService) *v1.VirtualService {
				vs.GetVirtualHost().Options = &gloov1.VirtualHostOptions{
					Transformations: nil,
				}
				return vs
			})

			EventuallyResponseTransformed(defaultPostBody, defaultOutput).Should(HaveOccurred())
		})

		It("should should transform json to html response on vhost", func() {
			testContext.PatchDefaultVirtualService(func(vs *v1.VirtualService) *v1.VirtualService {
				vs.GetVirtualHost().Options = &gloov1.VirtualHostOptions{
					Transformations: transform,
				}
				return vs
			})

			EventuallyResponseTransformed(defaultPostBody, defaultOutput).Should(Succeed())
		})

		It("should should transform json to html response on route", func() {
			testContext.PatchDefaultVirtualService(func(vs *v1.VirtualService) *v1.VirtualService {
				vs.GetVirtualHost().GetRoutes()[0].Options = &gloov1.RouteOptions{
					Transformations: transform,
				}
				return vs
			})

			EventuallyResponseTransformed(defaultPostBody, defaultOutput).Should(Succeed())
		})

		It("should should transform json to html response on route", func() {
			testContext.PatchDefaultVirtualService(func(vs *v1.VirtualService) *v1.VirtualService {
				vsBuilder := helpers.BuilderFromVirtualService(vs)
				vsBuilder.WithRouteActionToMultiDestination(e2e.DefaultRouteName, &gloov1.MultiDestination{
					Destinations: []*gloov1.WeightedDestination{{
						Weight: &wrappers.UInt32Value{Value: 1},
						Options: &gloov1.WeightedDestinationOptions{
							Transformations: transform,
						},
						Destination: &gloov1.Destination{
							DestinationType: &gloov1.Destination_Upstream{
								Upstream: testContext.TestUpstream().Upstream.GetMetadata().Ref(),
							},
						},
					}},
				})

				return vsBuilder.Build()
			})

			EventuallyResponseTransformed(defaultPostBody, defaultOutput).Should(Succeed())
		})

		When("constructing JSON body", func() {
			complexPostBody := `{"foo":"{\"nestedbar\":\"{\\\"deeply\\\":\\\"nested\\\"}\"}","bar":"\"bie\"","bas":"[\"eball\",\"ketball\"]"}`
			complexOutput := `{"FOO":"{\"nestedbar\":\"{\\\"deeply\\\":\\\"nested\\\"}\"}","BARBAS":["\"bie\"","[\"eball\",\"ketball\"]"]}`
			When("using escape_characters", func() {
				BeforeEach(func() {
					transform = &transformation.Transformations{
						ResponseTransformation: &transformation.Transformation{
							TransformationType: &transformation.Transformation_TransformationTemplate{
								TransformationTemplate: &transformation.TransformationTemplate{
									EscapeCharacters: &wrapperspb.BoolValue{Value: true},
									BodyTransformation: &transformation.TransformationTemplate_Body{
										Body: &transformation.InjaTemplate{
											Text: `{"FOO":"{{foo}}","BARBAS":["{{bar}}","{{bas}}"]}`,
										},
									},
								},
							},
						},
					}
				})

				When("setting escape_characters globally on settings", func() {
					BeforeEach(func() {
						testContext.SetRunSettings(&gloov1.Settings{
							Gloo: &gloov1.GlooOptions{
								TransformationEscapeCharacters: &wrapperspb.BoolValue{Value: true},
							},
						})

						transform.GetResponseTransformation().GetTransformationTemplate().EscapeCharacters = nil
					})

					It("should should transform json to json response on vhost", func() {
						testContext.PatchDefaultVirtualService(func(vs *v1.VirtualService) *v1.VirtualService {
							vs.GetVirtualHost().Options = &gloov1.VirtualHostOptions{
								Transformations: transform,
							}
							return vs
						})

						EventuallyResponseTransformed(complexPostBody, complexOutput).Should(Succeed())
					})

					It("should should transform json to json response on route", func() {
						testContext.PatchDefaultVirtualService(func(vs *v1.VirtualService) *v1.VirtualService {
							vs.GetVirtualHost().GetRoutes()[0].Options = &gloov1.RouteOptions{
								Transformations: transform,
							}
							return vs
						})

						EventuallyResponseTransformed(complexPostBody, complexOutput).Should(Succeed())
					})
				})
				It("should should transform json to json response on vhost", func() {
					testContext.PatchDefaultVirtualService(func(vs *v1.VirtualService) *v1.VirtualService {
						vs.GetVirtualHost().Options = &gloov1.VirtualHostOptions{
							Transformations: transform,
						}
						return vs
					})

					EventuallyResponseTransformed(complexPostBody, complexOutput).Should(Succeed())
				})

				It("should should transform json to json response on route", func() {
					testContext.PatchDefaultVirtualService(func(vs *v1.VirtualService) *v1.VirtualService {
						vs.GetVirtualHost().GetRoutes()[0].Options = &gloov1.RouteOptions{
							Transformations: transform,
						}
						return vs
					})

					EventuallyResponseTransformed(complexPostBody, complexOutput).Should(Succeed())
				})

			})

			When("using raw_string", func() {

				BeforeEach(func() {
					transform = &transformation.Transformations{
						ResponseTransformation: &transformation.Transformation{
							TransformationType: &transformation.Transformation_TransformationTemplate{
								TransformationTemplate: &transformation.TransformationTemplate{
									BodyTransformation: &transformation.TransformationTemplate_Body{
										Body: &transformation.InjaTemplate{
											Text: `{"FOO":"{{raw_string(foo)}}","BARBAS":["{{raw_string(bar)}}","{{raw_string(bas)}}"]}`,
										},
									},
								},
							},
						},
					}
				})
				It("should should transform json to json response on vhost", func() {
					testContext.PatchDefaultVirtualService(func(vs *v1.VirtualService) *v1.VirtualService {
						vs.GetVirtualHost().Options = &gloov1.VirtualHostOptions{
							Transformations: transform,
						}
						return vs
					})

					EventuallyResponseTransformed(complexPostBody, complexOutput).Should(Succeed())
				})

				It("should should transform json to json response on route", func() {
					testContext.PatchDefaultVirtualService(func(vs *v1.VirtualService) *v1.VirtualService {
						vs.GetVirtualHost().GetRoutes()[0].Options = &gloov1.RouteOptions{
							Transformations: transform,
						}
						return vs
					})

					EventuallyResponseTransformed(complexPostBody, complexOutput).Should(Succeed())
				})

			})
		})
	})

	Context("parsing non-valid JSON", func() {

		var transform *transformation.Transformations

		BeforeEach(func() {
			htmlResponse := "<html></html>"
			htmlEchoUpstream := v1helpers.NewTestHttpUpstreamWithReply(testContext.Ctx(), testContext.EnvoyInstance().LocalAddr(), htmlResponse)

			// This is a bit of a trick
			// We use the Default VirtualService name and then remove all VirtualServices in the ResourcesToCreate
			// This makes the vsToHtmlUpstream the "default" and tests can use PatchVirtualService to modify it
			vsToHtmlUpstream := helpers.NewVirtualServiceBuilder().
				WithName(e2e.DefaultVirtualServiceName).
				WithNamespace(writeNamespace).
				WithDomain(e2e.DefaultHost).
				WithRoutePrefixMatcher(e2e.DefaultRouteName, "/html").
				WithRouteActionToUpstream(e2e.DefaultRouteName, htmlEchoUpstream.Upstream).
				Build()

			testContext.ResourcesToCreate().Upstreams = gloov1.UpstreamList{htmlEchoUpstream.Upstream}
			testContext.ResourcesToCreate().VirtualServices = v1.VirtualServiceList{vsToHtmlUpstream}

			transform = &transformation.Transformations{
				ResponseTransformation: &transformation.Transformation{
					TransformationType: &transformation.Transformation_TransformationTemplate{
						TransformationTemplate: &transformation.TransformationTemplate{
							Headers: map[string]*transformation.InjaTemplate{
								"x-solo-resp-hdr1": {
									Text: "{{ request_header(\"x-solo-hdr-1\") }}",
								},
							},
						},
					},
				},
			}
		})

		// EventuallyHtmlResponseTransformed returns an Asynchronous Assertion which
		// validates that a request with a header will return a response header with the same
		// value, and the body of the response is non-json
		// This will only work if the above transformation is applied to the request
		EventuallyHtmlResponseTransformed := func() AsyncAssertion {
			htmlRequestBuilder := testContext.GetHttpRequestBuilder().
				WithPath("html").
				WithHeader("x-solo-hdr-1", "test")

			return Eventually(func(g Gomega) {
				g.Expect(testutils.DefaultHttpClient.Do(htmlRequestBuilder.Build())).To(testmatchers.HaveHttpResponse(&testmatchers.HttpResponse{
					StatusCode: http.StatusOK,
					Body: WithTransform(func(b []byte) error {
						var body map[string]interface{}
						return json.Unmarshal(b, &body)
					}, HaveOccurred()), // attempt to read body as json to confirm that it was not parsed
					Headers: map[string]interface{}{
						"x-solo-resp-hdr1": Equal("test"), // inspect response headers to confirm transformation was applied
					},
				}))
			}, "5s", ".5s")
		}

		It("should error on non-json body when ignoreErrorOnParse/parseBodyBehavior/passthrough is disabled", func() {
			transform.ResponseTransformation.GetTransformationTemplate().IgnoreErrorOnParse = false
			testContext.PatchDefaultVirtualService(func(vs *v1.VirtualService) *v1.VirtualService {
				vs.GetVirtualHost().Options = &gloov1.VirtualHostOptions{
					Transformations: transform,
				}
				return vs
			})

			htmlRequestBuilder := testContext.GetHttpRequestBuilder().
				WithPath("html").
				WithHeader("x-solo-hdr-1", "test")
			Eventually(func(g Gomega) {
				g.Expect(testutils.DefaultHttpClient.Do(htmlRequestBuilder.Build())).To(testmatchers.HaveHttpResponse(&testmatchers.HttpResponse{
					StatusCode: http.StatusBadRequest,
					Body:       gstruct.Ignore(), // We don't care about the body, which will contain an error message
				}))
			}, "5s", ".5s").Should(Succeed())
		})

		It("should transform response with non-json body when ignoreErrorOnParse is enabled", func() {
			transform.ResponseTransformation.GetTransformationTemplate().IgnoreErrorOnParse = true
			testContext.PatchDefaultVirtualService(func(vs *v1.VirtualService) *v1.VirtualService {
				vs.GetVirtualHost().Options = &gloov1.VirtualHostOptions{
					Transformations: transform,
				}
				return vs
			})

			EventuallyHtmlResponseTransformed().Should(Succeed())
		})

		It("should transform response with non-json body when ParseBodyBehavior is set to DontParse", func() {
			transform.ResponseTransformation.GetTransformationTemplate().ParseBodyBehavior = transformation.TransformationTemplate_DontParse
			testContext.PatchDefaultVirtualService(func(vs *v1.VirtualService) *v1.VirtualService {
				vs.GetVirtualHost().Options = &gloov1.VirtualHostOptions{
					Transformations: transform,
				}
				return vs
			})

			EventuallyHtmlResponseTransformed().Should(Succeed())
		})

		It("should transform response with non-json body when passthrough is enabled", func() {
			transform.ResponseTransformation.GetTransformationTemplate().BodyTransformation = &transformation.TransformationTemplate_Passthrough{
				Passthrough: &transformation.Passthrough{},
			}
			testContext.PatchDefaultVirtualService(func(vs *v1.VirtualService) *v1.VirtualService {
				vs.GetVirtualHost().Options = &gloov1.VirtualHostOptions{
					Transformations: transform,
				}
				return vs
			})

			EventuallyHtmlResponseTransformed().Should(Succeed())
		})
	})

	Context("requestTransformation", func() {

		// send the given request and assert that the response matches the given expected response
		eventuallyRequestMatches := func(req *http.Request, expectedResponse *testmatchers.HttpResponse) AsyncAssertion {
			return Eventually(func(g Gomega) {
				g.Expect(testutils.DefaultHttpClient.Do(req)).To(testmatchers.HaveHttpResponse(expectedResponse))
			}, "10s", ".5s")
		}

		BeforeEach(func() {
			// create a virtual host with a route to the upstream
			vsToEchoUpstream := helpers.NewVirtualServiceBuilder().
				WithName(e2e.DefaultVirtualServiceName).
				WithNamespace(writeNamespace).
				WithDomain(e2e.DefaultHost).
				WithRoutePrefixMatcher(e2e.DefaultRouteName, "/").
				WithRouteActionToUpstream(e2e.DefaultRouteName, testContext.TestUpstream().Upstream).
				WithVirtualHostOptions(&gloov1.VirtualHostOptions{
					StagedTransformations: &transformation.TransformationStages{
						Regular: &transformation.RequestResponseTransformations{
							RequestTransforms: []*transformation.RequestMatch{
								{
									RequestTransformation: &transformation.Transformation{
										TransformationType: &transformation.Transformation_HeaderBodyTransform{
											HeaderBodyTransform: &transformation.HeaderBodyTransform{
												AddRequestMetadata: true,
											},
										},
									},
								},
							},
						},
					},
				}).
				Build()

			testContext.ResourcesToCreate().VirtualServices = v1.VirtualServiceList{vsToEchoUpstream}
		})

		It("should handle queryStringParameters and multiValueQueryStringParameters", func() {
			// form request
			req := testContext.GetHttpRequestBuilder().
				WithPath("?foo=bar&multiple=1&multiple=2").
				Build()
			// form matcher
			matcher := &testmatchers.HttpResponse{
				StatusCode: http.StatusOK,
				Body: WithTransform(transforms.WithJsonBody(),
					And(
						HaveKeyWithValue("queryStringParameters", HaveKeyWithValue("foo", "bar")),
						HaveKeyWithValue("queryStringParameters", HaveKeyWithValue("multiple", "2")),
						HaveKeyWithValue("multiValueQueryStringParameters", HaveKeyWithValue("multiple", ConsistOf("1", "2"))),
					),
				),
			}

			eventuallyRequestMatches(req, matcher).Should(Succeed())
		})

		It("should handle 3 and 4 values in multiValueQueryStringParameters", func() {
			By("populating MultiValueQueryStringParameters with 3 values", func() {
				// form request
				req := testContext.GetHttpRequestBuilder().
					WithPath("?foo=bar&multiple=1&multiple=2&multiple=3").
					Build()
				// form matcher
				matcher := &testmatchers.HttpResponse{
					StatusCode: http.StatusOK,
					Body: WithTransform(transforms.WithJsonBody(),
						And(
							HaveKeyWithValue("queryStringParameters", HaveKeyWithValue("foo", "bar")),
							HaveKeyWithValue("queryStringParameters", HaveKeyWithValue("multiple", "3")),
							HaveKeyWithValue("multiValueQueryStringParameters", HaveKeyWithValue("multiple", ConsistOf("1", "2", "3"))),
						),
					),
				}

				eventuallyRequestMatches(req, matcher).Should(Succeed())
			})

			By("populating MultiValueQueryStringParameters with 4 values", func() {
				// form request
				req := testContext.GetHttpRequestBuilder().
					WithPath("?foo=bar&multiple=1&multiple=2&multiple=3&multiple=4").
					Build()
				// form matcher
				matcher := &testmatchers.HttpResponse{
					StatusCode: http.StatusOK,
					Body: WithTransform(transforms.WithJsonBody(),
						And(
							HaveKeyWithValue("queryStringParameters", HaveKeyWithValue("foo", "bar")),
							HaveKeyWithValue("queryStringParameters", HaveKeyWithValue("multiple", "4")), // last value
							HaveKeyWithValue("multiValueQueryStringParameters", HaveKeyWithValue("multiple", ConsistOf("1", "2", "3", "4"))),
						),
					),
				}

				eventuallyRequestMatches(req, matcher).Should(Succeed())
			})
		})

		It("should handle headers and multiValueHeaders", func() {
			// form request
			req := testContext.GetHttpRequestBuilder().
				WithHeader("foo", "bar").
				WithHeader("multiple", "1,2").
				Build()
			// form matcher
			matcher := &testmatchers.HttpResponse{
				StatusCode: http.StatusOK,
				Body: WithTransform(transforms.WithJsonBody(),
					And(
						HaveKeyWithValue("headers", HaveKeyWithValue("foo", "bar")),
						HaveKeyWithValue("headers", HaveKeyWithValue("multiple", "2")),
						HaveKeyWithValue("multiValueHeaders", HaveKeyWithValue("multiple", ConsistOf("1", "2"))),
					),
				),
			}

			eventuallyRequestMatches(req, matcher).Should(Succeed())
		})
	})
})
