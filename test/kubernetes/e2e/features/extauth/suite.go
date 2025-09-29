package extauth

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/onsi/gomega"
	"github.com/stretchr/testify/suite"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kgateway-dev/kgateway/v2/pkg/utils/kubeutils"
	"github.com/kgateway-dev/kgateway/v2/pkg/utils/requestutils/curl"
	testmatchers "github.com/kgateway-dev/kgateway/v2/test/gomega/matchers"
	"github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e"
	"github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e/defaults"
	testdefaults "github.com/kgateway-dev/kgateway/v2/test/kubernetes/e2e/defaults"
)

var _ e2e.NewSuiteFunc = NewTestingSuite

// testingSuite is a suite of tests for ExtAuth functionality
type testingSuite struct {
	suite.Suite

	ctx context.Context

	// testInstallation contains all the metadata/utilities necessary to execute a series of tests
	// against an installation of kgateway
	testInstallation *e2e.TestInstallation

	// manifests shared by all tests
	commonManifests []string
	// resources from manifests shared by all tests
	commonResources []client.Object
}

func NewTestingSuite(ctx context.Context, testInst *e2e.TestInstallation) suite.TestingSuite {
	return &testingSuite{
		ctx:              ctx,
		testInstallation: testInst,
	}
}

func (s *testingSuite) SetupSuite() {
	s.commonManifests = []string{
		testdefaults.CurlPodManifest,
		simpleServiceManifest,
		gatewayWithRouteManifest,
		extAuthManifest,
		// securedGatewayPolicyManifest,
		// securedRouteManifest,
		// insecureRouteManifest,
	}
	s.commonResources = []client.Object{
		// resources from curl manifest
		testdefaults.CurlPod,
		// resources from service manifest
		simpleSvc, simpleDeployment,
		// deployer-generated resources
		proxyDeployment, proxyService,
		// extauth resources
		extAuthSvc, extAuthExtension,
	}

	// set up common resources once
	for _, manifest := range s.commonManifests {
		err := s.testInstallation.Actions.Kubectl().ApplyFile(s.ctx, manifest)
		s.Require().NoError(err, "can apply "+manifest)
	}
	s.testInstallation.Assertions.EventuallyObjectsExist(s.ctx, s.commonResources...)

	// make sure pods are running
	s.testInstallation.Assertions.EventuallyPodsRunning(s.ctx, defaults.CurlPod.GetNamespace(), metav1.ListOptions{
		LabelSelector: defaults.CurlPodLabelSelector,
	})

	s.testInstallation.Assertions.EventuallyPodsRunning(s.ctx, proxyObjMeta.GetNamespace(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", proxyObjMeta.GetName()),
	}, time.Minute*2)
}

func (s *testingSuite) TearDownSuite() {
	// clean up common resources
	for _, manifest := range s.commonManifests {
		err := s.testInstallation.Actions.Kubectl().DeleteFileSafe(s.ctx, manifest)
		s.Require().NoError(err, "can delete "+manifest)
	}
	s.testInstallation.Assertions.EventuallyObjectsNotExist(s.ctx, s.commonResources...)

	s.testInstallation.Assertions.EventuallyPodsNotExist(s.ctx, proxyObjMeta.GetNamespace(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", proxyObjMeta.GetName()),
	})
}

// TestExtAuthPolicy tests the basic ExtAuth functionality with header-based allow/deny
// Checks for gateay level auth with route level opt out
func (s *testingSuite) TestExtAuthPolicy() {
	manifests := []string{
		securedGatewayPolicyManifest,
		insecureRouteManifest,
	}

	resources := []client.Object{
		gatewayAttachedTrafficPolicy,
		insecureRoute,
	}
	s.T().Cleanup(func() {
		for _, manifest := range manifests {
			err := s.testInstallation.Actions.Kubectl().DeleteFileSafe(s.ctx, manifest)
			s.Require().NoError(err)
		}
		s.testInstallation.Assertions.EventuallyObjectsNotExist(s.ctx, resources...)
	})
	// set up common resources once
	for _, manifest := range manifests {
		err := s.testInstallation.Actions.Kubectl().ApplyFile(s.ctx, manifest)
		s.Require().NoError(err, "can apply "+manifest)
	}
	s.testInstallation.Assertions.EventuallyObjectsExist(s.ctx, resources...)

	// Wait for pods to be running
	s.ensureBasicRunning()

	testCases := []struct {
		name                         string
		headers                      map[string]string
		hostname                     string
		expectedStatus               int
		expectedUpstreamBodyContents string
	}{
		{
			name: "request allowed with allow header",
			headers: map[string]string{
				"x-ext-authz": "allow",
			},
			hostname:                     "example.com",
			expectedStatus:               http.StatusOK,
			expectedUpstreamBodyContents: "X-Ext-Authz-Check-Result",
		},
		{
			name:           "request denied without allow header",
			headers:        map[string]string{},
			hostname:       "example.com",
			expectedStatus: http.StatusForbidden,
		},
		{
			name:     "request denied with deny header",
			hostname: "example.com",
			headers: map[string]string{
				"x-ext-authz": "deny",
			},
			expectedStatus: http.StatusForbidden,
		},
		{
			name:           "request allowed on insecure route",
			hostname:       "insecureroute.com",
			expectedStatus: http.StatusOK,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			// Build curl options
			opts := []curl.Option{
				curl.WithHost(kubeutils.ServiceFQDN(proxyObjMeta)),
				curl.WithHostHeader(tc.hostname),
				curl.WithPort(8080),
			}

			// Add test-specific headers
			for k, v := range tc.headers {
				opts = append(opts, curl.WithHeader(k, v))
			}

			// Test the request
			s.testInstallation.Assertions.AssertEventualCurlResponse(
				s.ctx,
				testdefaults.CurlPodExecOpt,
				opts,
				&testmatchers.HttpResponse{
					StatusCode: tc.expectedStatus,
					Body:       gomega.ContainSubstring(tc.expectedUpstreamBodyContents),
				})
		})
	}
}

// TextRouteTargetedExtAuthPolicy tests route level only extauth
func (s *testingSuite) TextRouteTargetedExtAuthPolicy() {
	manifests := []string{
		securedRouteManifest,
		secureAndDisableAllManifest,
		insecureRouteManifest,
	}

	resources := []client.Object{
		basicSecureRoute,
		insecureRoute, insecureTrafficPolicy,
	}
	s.T().Cleanup(func() {
		for _, manifest := range manifests {
			err := s.testInstallation.Actions.Kubectl().DeleteFileSafe(s.ctx, manifest)
			s.Require().NoError(err)
		}
		s.testInstallation.Assertions.EventuallyObjectsNotExist(s.ctx, resources...)
	})
	// set up common resources once
	for _, manifest := range manifests {
		err := s.testInstallation.Actions.Kubectl().ApplyFile(s.ctx, manifest)
		s.Require().NoError(err, "can apply "+manifest)
	}
	s.testInstallation.Assertions.EventuallyObjectsExist(s.ctx, resources...)

	// Wait for pods to be running
	s.ensureBasicRunning()

	testCases := []struct {
		name                         string
		headers                      map[string]string
		hostname                     string
		expectedStatus               int
		expectedUpstreamBodyContents string
	}{
		{
			name:           "request allowed by default",
			headers:        map[string]string{},
			hostname:       "example.com",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "request allowed on insecure route",
			hostname:       "insecureroute.com",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "request allowed on reinsecured route",
			hostname:       "disableall.com",
			expectedStatus: http.StatusOK,
		},
		{
			name: "request allowed with allow header on secured route",
			headers: map[string]string{
				"x-ext-authz": "allow",
			},
			hostname:                     "securedroute.com",
			expectedStatus:               http.StatusOK,
			expectedUpstreamBodyContents: "X-Ext-Authz-Check-Result",
		},
		{
			name:           "request denied without header on secured route",
			hostname:       "securedroute.com",
			headers:        map[string]string{},
			expectedStatus: http.StatusForbidden,
		},
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			// Build curl options
			opts := []curl.Option{
				curl.WithHost(kubeutils.ServiceFQDN(proxyObjMeta)),
				curl.WithHostHeader(tc.hostname),
				curl.WithPort(8080),
			}

			// Add test-specific headers
			for k, v := range tc.headers {
				opts = append(opts, curl.WithHeader(k, v))
			}

			// Test the request
			s.testInstallation.Assertions.AssertEventualCurlResponse(
				s.ctx,
				testdefaults.CurlPodExecOpt,
				opts,
				&testmatchers.HttpResponse{
					StatusCode: tc.expectedStatus,
					Body:       gomega.ContainSubstring(tc.expectedUpstreamBodyContents),
				})
		})
	}
}

func (s *testingSuite) ensureBasicRunning() {
	s.testInstallation.Assertions.EventuallyPodsRunning(s.ctx, testdefaults.CurlPod.GetNamespace(), metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=curl",
	})
	s.testInstallation.Assertions.EventuallyPodsRunning(s.ctx, proxyObjMeta.GetNamespace(), metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=super-gateway",
	}, time.Minute)
	s.testInstallation.Assertions.EventuallyPodsRunning(s.ctx, extAuthSvc.GetNamespace(), metav1.ListOptions{
		LabelSelector: "app=ext-authz",
	})
}
