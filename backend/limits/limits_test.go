package limits

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gofr.dev/pkg/gofr"
)

func TestUnlimited_AllowsEverything(t *testing.T) {
	assert.NoError(t, Unlimited{}.CanCreateOrg(nil, 1))
}

func TestUnimplementedPolicy_DefaultsToAllow(t *testing.T) {
	assert.NoError(t, UnimplementedPolicy{}.CanCreateOrg(nil, 1))
}

// An implementation embeds UnimplementedPolicy and overrides only the checks
// it enforces — the forward-compatibility pattern the package requires.
type denyOrgs struct {
	UnimplementedPolicy
}

func (denyOrgs) CanCreateOrg(*gofr.Context, int64) error { return Deny("no more orgs") }

func TestEmbeddedOverride_Denies(t *testing.T) {
	var p Policy = denyOrgs{}

	err := p.CanCreateOrg(nil, 1)

	assert.Equal(t, Deny("no more orgs"), err)
	assert.Equal(t, "no more orgs", err.Error())
}
