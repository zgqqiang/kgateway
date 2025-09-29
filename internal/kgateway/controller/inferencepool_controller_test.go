package controller_test

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	infextv1a2 "sigs.k8s.io/gateway-api-inference-extension/api/v1alpha2"
	apiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kgateway-dev/kgateway/v2/internal/kgateway/deployer"
)

var _ = Describe("InferencePool controller", func() {
	AfterEach(func() {
		if cancel != nil {
			cancel()
		}
		// ensure goroutines cleanup
		Eventually(func() bool { return true }).WithTimeout(3 * time.Second).Should(BeTrue())
	})

	Context("when Inference Extension deployer is enabled", func() {
		BeforeEach(func() {
			var err error
			inferenceExt = new(deployer.InferenceExtInfo)
			cancel, err = createManager(ctx, inferenceExt, nil)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should reconcile an InferencePool referenced by a managed HTTPRoute and deploy the endpoint picker", func() {
			// Create a test Gateway that will be referenced by the HTTPRoute.
			testGw := &apiv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-gateway",
					Namespace: defaultNamespace,
				},
				Spec: apiv1.GatewaySpec{
					GatewayClassName: gatewayClassName,
					Listeners: []apiv1.Listener{
						{
							Name:     "listener-1",
							Protocol: apiv1.HTTPProtocolType,
							Port:     80,
						},
					},
				},
			}
			err := k8sClient.Create(ctx, testGw)
			Expect(err).NotTo(HaveOccurred())

			// Create an HTTPRoute without a status.
			httpRoute := &apiv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route",
					Namespace: defaultNamespace,
				},
				Spec: apiv1.HTTPRouteSpec{
					Rules: []apiv1.HTTPRouteRule{
						{
							BackendRefs: []apiv1.HTTPBackendRef{
								{
									BackendRef: apiv1.BackendRef{
										BackendObjectReference: apiv1.BackendObjectReference{
											Group: ptr.To(apiv1.Group(infextv1a2.GroupVersion.Group)),
											Kind:  ptr.To(apiv1.Kind("InferencePool")),
											Name:  "pool1",
										},
									},
								},
							},
						},
					},
				},
			}
			err = k8sClient.Create(ctx, httpRoute)
			Expect(err).NotTo(HaveOccurred())

			// Now update the status to include a valid Parents field.
			httpRoute.Status = apiv1.HTTPRouteStatus{
				RouteStatus: apiv1.RouteStatus{
					Parents: []apiv1.RouteParentStatus{
						{
							ParentRef: apiv1.ParentReference{
								Group:     ptr.To(apiv1.Group("gateway.networking.k8s.io")),
								Kind:      ptr.To(apiv1.Kind("Gateway")),
								Name:      apiv1.ObjectName(testGw.Name),
								Namespace: ptr.To(apiv1.Namespace(defaultNamespace)),
							},
							ControllerName: gatewayControllerName,
						},
					},
				},
			}
			Eventually(func() error {
				return k8sClient.Status().Update(ctx, httpRoute)
			}, "10s", "1s").Should(Succeed())

			// Create an InferencePool resource that is referenced by the HTTPRoute.
			pool := &infextv1a2.InferencePool{
				TypeMeta: metav1.TypeMeta{
					Kind:       "InferencePool",
					APIVersion: infextv1a2.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pool1",
					Namespace: defaultNamespace,
					UID:       "pool-uid",
				},
				Spec: infextv1a2.InferencePoolSpec{
					Selector:         map[infextv1a2.LabelKey]infextv1a2.LabelValue{},
					TargetPortNumber: 1234,
					EndpointPickerConfig: infextv1a2.EndpointPickerConfig{
						ExtensionRef: &infextv1a2.Extension{
							ExtensionReference: infextv1a2.ExtensionReference{
								Name: "doesnt-matter",
							},
						},
					},
				},
			}
			err = k8sClient.Create(ctx, pool)
			Expect(err).NotTo(HaveOccurred())

			// The secondary watch on HTTPRoute should now trigger reconciliation of pool "pool1".
			// We expect the deployer to render and deploy an endpoint picker Deployment with name "pool1-endpoint-picker".
			expectedName := fmt.Sprintf("%s-endpoint-picker", pool.Name)
			var deploy appsv1.Deployment
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Namespace: defaultNamespace, Name: expectedName}, &deploy)
			}, "10s", "1s").Should(Succeed())
		})

		It("should ignore an InferencePool not referenced by any HTTPRoute and not deploy the endpoint picker", func() {
			// Create an InferencePool that is not referenced by any HTTPRoute.
			pool := &infextv1a2.InferencePool{
				TypeMeta: metav1.TypeMeta{
					Kind:       "InferencePool",
					APIVersion: infextv1a2.GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pool2",
					Namespace: defaultNamespace,
					UID:       "pool2-uid",
				},
				Spec: infextv1a2.InferencePoolSpec{
					Selector:         map[infextv1a2.LabelKey]infextv1a2.LabelValue{},
					TargetPortNumber: 1234,
					EndpointPickerConfig: infextv1a2.EndpointPickerConfig{
						ExtensionRef: &infextv1a2.Extension{
							ExtensionReference: infextv1a2.ExtensionReference{
								Name: "doesnt-matter",
							},
						},
					},
				},
			}
			err := k8sClient.Create(ctx, pool)
			Expect(err).NotTo(HaveOccurred())

			// Consistently check that no endpoint picker deployment is created.
			Consistently(func() error {
				var dep appsv1.Deployment
				return k8sClient.Get(ctx, client.ObjectKey{Namespace: defaultNamespace, Name: fmt.Sprintf("%s-endpoint-picker", pool.Name)}, &dep)
			}, "5s", "1s").ShouldNot(Succeed())
		})
	})

	Context("when Inference Extension deployer is disabled", func() {
		BeforeEach(func() {
			var err error
			inferenceExt = nil
			cancel, err = createManager(ctx, inferenceExt, nil)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should not deploy endpoint picker resources", func() {
			httpRoute := &apiv1.HTTPRoute{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-route-disabled",
					Namespace: defaultNamespace,
				},
				Spec: apiv1.HTTPRouteSpec{
					Rules: []apiv1.HTTPRouteRule{{
						BackendRefs: []apiv1.HTTPBackendRef{{
							BackendRef: apiv1.BackendRef{
								BackendObjectReference: apiv1.BackendObjectReference{
									Group: ptr.To(apiv1.Group(infextv1a2.GroupVersion.Group)),
									Kind:  ptr.To(apiv1.Kind("InferencePool")),
									Name:  "pool-disabled",
								},
							},
						}},
					}},
				},
			}
			Expect(k8sClient.Create(ctx, httpRoute)).To(Succeed())

			pool := &infextv1a2.InferencePool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pool-disabled",
					Namespace: defaultNamespace,
				},
				Spec: infextv1a2.InferencePoolSpec{
					Selector:         map[infextv1a2.LabelKey]infextv1a2.LabelValue{},
					TargetPortNumber: 1234,
					EndpointPickerConfig: infextv1a2.EndpointPickerConfig{
						ExtensionRef: &infextv1a2.Extension{
							ExtensionReference: infextv1a2.ExtensionReference{Name: "doesnt-matter"},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pool)).To(Succeed())

			expectedName := fmt.Sprintf("%s-endpoint-picker", pool.Name)
			Consistently(func() error {
				var dep appsv1.Deployment
				return k8sClient.Get(ctx, client.ObjectKey{Namespace: defaultNamespace, Name: expectedName}, &dep)
			}, "5s", "1s").ShouldNot(Succeed())
		})
	})
})
