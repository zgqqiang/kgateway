//go:build ignore

package gloo_test

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/solo-io/solo-kit/test/helpers"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/solo-io/solo-kit/pkg/api/v1/clients"
	kubecache "github.com/solo-io/solo-kit/pkg/api/v1/clients/kube/cache"
	"github.com/solo-io/solo-kit/pkg/api/v1/resources/core"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/api/v1"
	kubepluginapi "github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/api/v1/options/kubernetes"
	"github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/discovery"
	kubeplugin "github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/plugins/kubernetes"

	"k8s.io/client-go/kubernetes"
)

// Kubernetes tests for plugin from projects/gloo/pkg/plugins/kubernetes
var _ = Describe("Kubernetes Plugin", func() {

	var (
		svcNamespace  string
		svcName       = "i-love-writing-tests"
		kubeClient    kubernetes.Interface
		kubeCoreCache kubecache.KubeCoreCache

		baseLabels = map[string]string{
			"tacos": "burritos",
		}

		extendedLabels = map[string]string{
			"tacos": "burritos",
			"pizza": "frenchfries",
		}
	)

	BeforeEach(func() {
		var err error

		svcNamespace = helpers.RandString(8)
		kubeClient = resourceClientset.KubeClients()

		kubeCoreCache, err = kubecache.NewKubeCoreCache(ctx, kubeClient)
		Expect(err).NotTo(HaveOccurred())

		_, err = kubeClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: svcNamespace,
			},
		}, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		// create a service
		// create 2 pods for that service
		// one with extra labels, one without
		svc, err := kubeClient.CoreV1().Services(svcNamespace).Create(ctx, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: svcNamespace,
				Name:      svcName,
			},
			Spec: corev1.ServiceSpec{
				Selector: baseLabels,
				Ports: []corev1.ServicePort{
					{
						Name: "bar",
						Port: 8080,
					},
					{
						Name: "foo",
						Port: 9090,
					},
				},
			},
		}, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		_, err = kubeClient.CoreV1().Pods(svcNamespace).Create(ctx, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-for-" + svc.Name + "-basic",
				Namespace: svcNamespace,
				Labels:    baseLabels,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "nginx",
						Image: "nginx:latest",
					},
				},
			},
		}, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		_, err = kubeClient.CoreV1().Pods(svcNamespace).Create(ctx, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod-for-" + svc.Name + "-extra",
				Namespace: svcNamespace,
				Labels:    extendedLabels,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "nginx",
						Image: "nginx:latest",
					},
				},
			},
		}, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())
		_, err = kubeClient.CoreV1().Endpoints(svcNamespace).Update(ctx, &corev1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svc.Name,
				Namespace: svcNamespace,
			},
			Subsets: []corev1.EndpointSubset{{
				Addresses: []corev1.EndpointAddress{
					{IP: "10.4.0.60"},
					{IP: "10.4.0.61"},
				},
				Ports: []corev1.EndpointPort{
					{Name: "foo", Port: 9090, Protocol: corev1.ProtocolTCP},
					{Name: "bar", Port: 8080, Protocol: corev1.ProtocolTCP},
				},
			}},
		}, metav1.UpdateOptions{})
		Expect(err).NotTo(HaveOccurred())

		// Wait for created pods to be running
		Eventually(func(g Gomega) {
			pods, err := kubeClient.CoreV1().Pods(svcNamespace).List(ctx, metav1.ListOptions{
				LabelSelector: labels.SelectorFromSet(baseLabels).String(),
			})
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(pods.Items).To(HaveLen(2), "We only expect 2 pods with these labels")
			for _, pod := range pods.Items {
				g.Expect(pod.Status.Phase).To(Equal(corev1.PodRunning), "pod should be running")
			}
		}, 60*time.Second, 1*time.Second).ShouldNot(HaveOccurred())

	})

	AfterEach(func() {
		err := kubeClient.CoreV1().Namespaces().Delete(ctx, svcNamespace, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())
	})

	It("uses json keys when serializing", func() {
		plug := kubeplugin.NewPlugin(kubeClient, kubeCoreCache, nil).(discovery.DiscoveryPlugin)
		upstreams, errs, err := plug.DiscoverUpstreams([]string{svcNamespace}, svcNamespace, clients.WatchOpts{
			Ctx:         context.TODO(),
			RefreshRate: time.Second,
		}, discovery.Opts{})
		Expect(err).NotTo(HaveOccurred())

		select {
		case <-time.After(time.Second * 2):
			Fail("no upstreams detected after 2s")
		case upstreamList := <-upstreams:
			// two pods, two ports per pod. both pods selected by a single service
			// create an upstream for each port on the service (2)
			Expect(upstreamList).To(HaveLen(2))
			break
		case err, ok := <-errs:
			if !ok {
				return
			}
			Expect(err).NotTo(HaveOccurred())
		}
	})

	It("shares endpoints between multiple upstreams that have the same endpoint", func() {
		makeUpstream := func(name string) *v1.Upstream {
			return &v1.Upstream{
				Metadata: &core.Metadata{Name: name},
				UpstreamType: &v1.Upstream_Kube{
					Kube: &kubepluginapi.UpstreamSpec{
						ServiceNamespace: svcNamespace,
						ServiceName:      svcName,
						ServicePort:      8080,
					},
				},
			}
		}
		plug := kubeplugin.NewPlugin(kubeClient, kubeCoreCache, nil).(discovery.DiscoveryPlugin)
		endpoints, errs, err := plug.WatchEndpoints(
			"",
			v1.UpstreamList{makeUpstream("a"), makeUpstream("b"), makeUpstream("c")},
			clients.WatchOpts{
				Ctx:         ctx,
				RefreshRate: time.Second,
			})
		Expect(err).NotTo(HaveOccurred())

		select {
		case <-time.After(time.Second * 2):
			Fail("no endpoints detected after 2s")
		case endpointList := <-endpoints:
			Expect(endpointList).To(HaveLen(6))
			break
		case err, ok := <-errs:
			if !ok {
				return
			}
			Expect(err).NotTo(HaveOccurred())
		}
	})
})
