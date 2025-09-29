//go:build ignore

package helpers_test

import (
	"testing"

	"github.com/solo-io/go-utils/testutils"

	. "github.com/onsi/ginkgo/v2"
)

func TestHelpers(t *testing.T) {
	testutils.RegisterCommonFailHandlers()
	RunSpecs(t, "Helpers Suite")
}
