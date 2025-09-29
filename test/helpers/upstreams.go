//go:build ignore

package helpers

import (
	"fmt"

	"github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/api/external/envoy/api/v2/core"
	v1 "github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/api/v1"
	"github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/api/v1/ssl"
)

// UpstreamBuilder contains options for building Upstreams to be included in scaled Snapshots
type UpstreamBuilder struct {
	sniPattern sniPattern

	healthChecks []*core.HealthCheck
}

type sniPattern int

const (
	noSni sniPattern = iota
	uniqueSni
	consistentSni
)

func NewUpstreamBuilder() *UpstreamBuilder {
	return &UpstreamBuilder{}
}

func (b *UpstreamBuilder) WithUniqueSni() *UpstreamBuilder {
	b.sniPattern = uniqueSni
	return b
}

func (b *UpstreamBuilder) WithConsistentSni() *UpstreamBuilder {
	b.sniPattern = consistentSni
	return b
}

func (b *UpstreamBuilder) WithHealthChecks(healthChecks []*core.HealthCheck) *UpstreamBuilder {
	b.healthChecks = healthChecks
	return b
}

func (b *UpstreamBuilder) Build(i int) *v1.Upstream {
	up := Upstream(i)

	up.HealthChecks = b.healthChecks

	switch b.sniPattern {
	case uniqueSni:
		up.SslConfig = &ssl.UpstreamSslConfig{
			Sni: fmt.Sprintf("unique-domain-%d", i),
		}
	case consistentSni:
		up.SslConfig = &ssl.UpstreamSslConfig{
			Sni: "consistent-domain",
		}
	}

	return up
}
