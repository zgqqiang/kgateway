//go:build ignore

package helpers

import v1 "github.com/kgateway-dev/kgateway/v2/internal/gloo/pkg/api/v1"

// SecretBuilder contains options for building Secrets to be included in scaled Snapshots
// there are no options currently configurable for the SecretBuilder
type SecretBuilder struct{}

func NewSecretBuilder() *SecretBuilder {
	return &SecretBuilder{}
}

func (b *SecretBuilder) Build(i int) *v1.Secret {
	return Secret(i)
}
