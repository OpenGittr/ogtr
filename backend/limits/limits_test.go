package limits

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gofr.dev/pkg/gofr"
)

// Every axis of the default policies answers "allow"; the analytics window is
// the unbounded zero value.
func TestDefaultPoliciesAllowEveryAxis(t *testing.T) {
	for _, p := range []Policy{Unlimited{}, UnimplementedPolicy{}} {
		assert.NoError(t, p.CanCreateOrg(nil, 1))
		assert.NoError(t, p.CanCreateLink(nil, 1, 2))
		assert.NoError(t, p.CanCreateLink(nil, 1, 0), "API-key creation (userID 0) allows too")
		assert.NoError(t, p.CanAddDomain(nil, 1))
		assert.NoError(t, p.CanAddMember(nil, 1))
		assert.NoError(t, p.CanCreateAPIKey(nil, 1))

		window, err := p.AnalyticsWindow(nil, 1)
		require.NoError(t, err)
		assert.Equal(t, Window{}, window, "the default window is fully unbounded")
	}
}

func TestWindowZeroValueMeansUnbounded(t *testing.T) {
	var w Window

	assert.Zero(t, w.ViewableEvents)
	assert.Zero(t, w.Retention)
	assert.Empty(t, w.Message)
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

// Overriding one axis leaves every other axis (including ones added later)
// falling through to the always-allow base.
func TestEmbeddedOverride_OtherAxesStillAllow(t *testing.T) {
	var p Policy = denyOrgs{}

	assert.Error(t, p.CanCreateOrg(nil, 1))

	assert.NoError(t, p.CanCreateLink(nil, 1, 2))
	assert.NoError(t, p.CanAddDomain(nil, 1))
	assert.NoError(t, p.CanAddMember(nil, 1))
	assert.NoError(t, p.CanCreateAPIKey(nil, 1))

	window, err := p.AnalyticsWindow(nil, 1)
	require.NoError(t, err)
	assert.Equal(t, Window{}, window)
}

// boundedAnalytics overrides only the analytics window.
type boundedAnalytics struct {
	UnimplementedPolicy
}

func (boundedAnalytics) AnalyticsWindow(*gofr.Context, int64) (Window, error) {
	return Window{ViewableEvents: 1000, Retention: 30 * 24 * time.Hour, Message: "over the viewable bound"}, nil
}

func TestEmbeddedOverride_AnalyticsWindow(t *testing.T) {
	var p Policy = boundedAnalytics{}

	window, err := p.AnalyticsWindow(nil, 1)

	require.NoError(t, err)
	assert.Equal(t, int64(1000), window.ViewableEvents)
	assert.Equal(t, 30*24*time.Hour, window.Retention)
	assert.Equal(t, "over the viewable bound", window.Message)

	assert.NoError(t, p.CanCreateLink(nil, 1, 2), "resource axes stay allowed")
}
