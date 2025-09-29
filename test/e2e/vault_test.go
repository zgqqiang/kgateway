//go:build ignore

package e2e_test

import (
	"github.com/solo-io/solo-kit/pkg/api/v1/clients"
	"github.com/solo-io/solo-kit/pkg/api/v1/resources/core"

	gloov1 "github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/api/v1"
	v1 "github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/api/v1/enterprise/options/extauth/v1"
	bootstrap "github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/bootstrap/clients"
	"github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/bootstrap/clients/vault"
	"github.com/kgateway-dev/kgateway/v2/test/e2e"
	"github.com/kgateway-dev/kgateway/v2/test/ginkgo/decorators"
	"github.com/kgateway-dev/kgateway/v2/test/services"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Vault Secret Store (Token Auth)", decorators.Vault, func() {

	var (
		testContext *e2e.TestContextWithVault
	)

	BeforeEach(func() {
		testContext = testContextFactory.NewTestContextWithVault()
		testContext.BeforeEach()

		testContext.SetRunSettings(&gloov1.Settings{
			SecretSource: &gloov1.Settings_VaultSecretSource{
				VaultSecretSource: &gloov1.Settings_VaultSecrets{
					Address: testContext.VaultInstance().Address(),
					AuthMethod: &gloov1.Settings_VaultSecrets_AccessToken{
						AccessToken: services.DefaultVaultToken,
					},
					PathPrefix: vault.DefaultPathPrefix,
					RootKey:    bootstrap.DefaultRootKey,
				},
			},
		})

		testContext.RunVault()
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

	Context("Oauth Secret", func() {

		var (
			oauthSecret *gloov1.Secret
		)

		BeforeEach(func() {
			oauthSecret = &gloov1.Secret{
				Metadata: &core.Metadata{
					Name:      "oauth-secret",
					Namespace: writeNamespace,
				},
				Kind: &gloov1.Secret_Oauth{
					Oauth: &v1.OauthSecret{
						ClientSecret: "original-secret",
					},
				},
			}

			testContext.ResourcesToCreate().Secrets = gloov1.SecretList{
				oauthSecret,
			}
		})

		It("can read secret using resource client", func() {
			Eventually(func(g Gomega) {
				secret, err := testContext.TestClients().SecretClient.Read(
					oauthSecret.GetMetadata().GetNamespace(),
					oauthSecret.GetMetadata().GetName(),
					clients.ReadOpts{
						Ctx: testContext.Ctx(),
					})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(secret.GetOauth().GetClientSecret()).To(Equal("original-secret"))
			}, "5s", ".5s").Should(Succeed())
		})

		It("can pick up new secrets created by vault client ", func() {
			newSecret := &gloov1.Secret{
				Metadata: &core.Metadata{
					Name:      "new-secret",
					Namespace: writeNamespace,
				},
				Kind: &gloov1.Secret_Oauth{
					Oauth: &v1.OauthSecret{
						ClientSecret: "new-secret",
					},
				},
			}

			err := testContext.VaultInstance().WriteSecret(newSecret)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func(g Gomega) {
				secret, err := testContext.TestClients().SecretClient.Read(
					newSecret.GetMetadata().GetNamespace(),
					newSecret.GetMetadata().GetName(),
					clients.ReadOpts{
						Ctx: testContext.Ctx(),
					})
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(secret.GetOauth().GetClientSecret()).To(Equal("new-secret"))
			}, "5s", ".5s").Should(Succeed())
		})

	})

})
