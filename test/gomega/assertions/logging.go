//go:build ignore

package assertions

import (
	"fmt"
	"net/http"

	"github.com/kgateway-dev/kgateway/v2/test/testutils"

	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/solo-io/go-utils/stats"
	"go.uber.org/zap/zapcore"

	"github.com/kgateway-dev/kgateway/v2/test/gomega/matchers"
)

// LogLevelAssertion returns an Assertion to verify that the dynamic log level matches the provided value
func LogLevelAssertion(logLevel zapcore.Level) types.AsyncAssertion {
	loggingRequest := testutils.DefaultRequestBuilder().
		WithPort(stats.DefaultPort).
		WithPath("logging").
		Build()

	return Eventually(func(g Gomega) {
		resp, err := http.DefaultClient.Do(loggingRequest)
		g.Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		g.Expect(resp).Should(matchers.HaveHttpResponse(&matchers.HttpResponse{
			StatusCode: http.StatusOK,
			Body:       fmt.Sprintf("{\"level\":\"%s\"}\n", logLevel.String()),
		}))
	}, "5s", ".1s")
}
