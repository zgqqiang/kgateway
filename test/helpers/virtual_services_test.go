//go:build ignore

package helpers_test

import (
	"hash"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/solo-io/solo-kit/test/matchers"

	gatewayv1 "github.com/kgateway-dev/kgateway/v2/internal/gateway/pkg/api/v1"
	gloov1 "github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/api/v1"
	"github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/api/v1/options/cors"
	"github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/api/v1/ssl"
	"github.com/kgateway-dev/kgateway/v2/test/gomega/assertions"
	"github.com/kgateway-dev/kgateway/v2/test/helpers"
)

var _ = Describe("VirtualServiceBuilder", func() {

	It("will fail if the virtual service builder has a new top level field", func() {
		// This test is important as it checks whether the virtual service builder has a new top level field.
		// This should happen very rarely, and should be used as an indication that the `Clone` function
		// most likely needs to change to support this new field
		assertions.ExpectNumFields(helpers.VirtualServiceBuilder{}, 7)
	})

	It("clones all fields", func() {
		originalBuilder := helpers.NewVirtualServiceBuilder().
			WithName("original-name").
			WithNamespace("original-namespace").
			WithLabel("original-label", "original-value").
			WithDomain("original.com").
			WithVirtualHostOptions(&gloov1.VirtualHostOptions{
				Cors: &cors.CorsPolicy{
					AllowCredentials: true,
				},
			}).
			WithRoute("original-route", &gatewayv1.Route{
				Name: "original-route",
			}).
			WithSslConfig(&ssl.SslConfig{
				SniDomains: []string{"original.com"},
			})

		clonedBuilder := originalBuilder.Clone().
			WithName("cloned-name").
			WithNamespace("cloned-namespace").
			WithLabel("original-label", "cloned-value").
			WithDomain("cloned.com").
			WithVirtualHostOptions(&gloov1.VirtualHostOptions{
				Cors: &cors.CorsPolicy{
					AllowCredentials: false,
				},
			}).
			// It's important that we update the original-route with a new name
			WithRoute("original-route", &gatewayv1.Route{
				Name: "cloned-route",
			}).
			WithSslConfig(&ssl.SslConfig{
				SniDomains: []string{"cloned.com"},
			})

		// Cloning the originalBuilder means we should be modifying only the clone, and not the original originalBuilder
		// Below we assert that each field in the generated VirtualService matches the value from the builder

		originalVirtualService := originalBuilder.Build()
		clonedVirtualService := clonedBuilder.Build()
		Expect(originalVirtualService.GetMetadata().GetName()).To(Equal("original-name"))
		Expect(clonedVirtualService.GetMetadata().GetName()).To(Equal("cloned-name"))

		Expect(originalVirtualService.GetMetadata().GetNamespace()).To(Equal("original-namespace"))
		Expect(clonedVirtualService.GetMetadata().GetNamespace()).To(Equal("cloned-namespace"))

		Expect(originalVirtualService.GetMetadata().GetLabels()["original-label"]).To(Equal("original-value"))
		Expect(clonedVirtualService.GetMetadata().GetLabels()["original-label"]).To(Equal("cloned-value"))

		Expect(originalVirtualService.GetSslConfig().GetSniDomains()).To(Equal([]string{"original.com"}))
		Expect(clonedVirtualService.GetSslConfig().GetSniDomains()).To(Equal([]string{"cloned.com"}))

		originalVirtualHost := originalVirtualService.GetVirtualHost()
		clonedVirtualHost := clonedVirtualService.GetVirtualHost()
		Expect(originalVirtualHost.GetDomains()).To(Equal([]string{"original.com"}))
		Expect(clonedVirtualHost.GetDomains()).To(Equal([]string{"cloned.com"}))

		Expect(originalVirtualHost.GetOptions().GetCors().GetAllowCredentials()).To(BeTrue())
		Expect(clonedVirtualHost.GetOptions().GetCors().GetAllowCredentials()).To(BeFalse())

		Expect(originalVirtualHost.GetRoutes()).To(HaveLen(1))
		Expect(originalVirtualHost.GetRoutes()[0]).To(matchers.MatchProto(&gatewayv1.Route{
			Name: "original-route",
		}))
		Expect(originalVirtualHost.GetRoutes()).To(HaveLen(1))
		Expect(clonedVirtualHost.GetRoutes()[0]).To(matchers.MatchProto(&gatewayv1.Route{
			Name: "cloned-route",
		}))
	})

	It("hashes virtual services with different labels to different values", func() {
		var hasher hash.Hash64

		originalBuilder := helpers.NewVirtualServiceBuilder().
			WithName("original-name").
			WithNamespace("original-namespace").
			WithLabel("original-label", "original-value").
			WithDomain("original.com")
		modifiedLabelBuilder := originalBuilder.Clone().
			WithLabel("original-label", "modified-value")

		originalVsHash, err := originalBuilder.Build().Hash(hasher)
		Expect(err).NotTo(HaveOccurred())

		vsWithModifiedLabelHash, err := modifiedLabelBuilder.Build().Hash(hasher)
		Expect(err).NotTo(HaveOccurred())

		Expect(originalVsHash).NotTo(Equal(vsWithModifiedLabelHash), "Hashes should be different, due to label differences")
	})

})
