//go:build ignore

package parallel_test

import (
	"testing"

	"github.com/solo-io/go-utils/testutils"

	. "github.com/onsi/ginkgo/v2"
)

func TestParallel(t *testing.T) {
	testutils.RegisterCommonFailHandlers()
	RunSpecs(t, "Parallel Suite")
}
