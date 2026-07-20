package services

import (
	"context"
	"net"
	"net/http"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"

	"github.com/opengittr/ogtr/backend/limits"
	"github.com/opengittr/ogtr/backend/models"
)

type domainMocks struct {
	domains *MockDomainStore
	members *MockMemberStore
	dns     *MockDNSResolver
}

// newDomainService wires a DomainService against mocks. SHORT_DOMAIN carries
// a port like local dev does — the service must compare hosts only.
func newDomainService(t *testing.T) (*DomainService, domainMocks, *gofr.Context) {
	t.Helper()

	return newDomainServiceWithPolicy(t, limits.Unlimited{})
}

func newDomainServiceWithPolicy(t *testing.T, policy limits.Policy) (*DomainService, domainMocks, *gofr.Context) {
	t.Helper()

	ctrl := gomock.NewController(t)
	m := domainMocks{
		domains: NewMockDomainStore(ctrl),
		members: NewMockMemberStore(ctrl),
		dns:     NewMockDNSResolver(ctrl),
	}

	mockContainer, _ := container.NewMockContainer(t)
	ctx := &gofr.Context{Context: context.Background(), Container: mockContainer}

	return NewDomainService(m.domains, m.members, m.dns, policy, "sho.rt:5810"), m, ctx
}

func stubOwner(m domainMocks) {
	m.members.EXPECT().GetRole(gomock.Any(), int64(3), int64(7)).Return(models.RoleOwner, nil)
}

func pendingDomain(id int64, hostname string) *models.Domain {
	return &models.Domain{
		ID: id, OrgID: 3, Hostname: hostname,
		VerificationToken: "tok1234567890tok1234567890tok123",
		Status:            models.DomainStatusPending,
	}
}

func verifiedDomain(id int64, hostname string) *models.Domain {
	d := pendingDomain(id, hostname)
	d.Status = models.DomainStatusVerified

	return d
}

var tokenRe = regexp.MustCompile(`^[a-zA-Z0-9]{32}$`)

func TestDomainService_Create_NormalizesHostname(t *testing.T) {
	tests := []struct {
		desc  string
		input string
		want  string
	}{
		{"plain hostname", "links.example.com", "links.example.com"},
		{"uppercase and whitespace", "  LINKS.Example.COM  ", "links.example.com"},
		{"trailing dot", "links.example.com.", "links.example.com"},
		{"IDN is punycode-normalized", "münchen.example.com", "xn--mnchen-3ya.example.com"},
		{"short-domain lookalike is not a subdomain", "mysho.rt", "mysho.rt"},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, m, ctx := newDomainService(t)
			stubOwner(m)
			m.domains.EXPECT().GetByHostname(gomock.Any(), tc.want).Return(nil, nil)
			m.domains.EXPECT().Create(gomock.Any(), int64(3), tc.want, gomock.Any()).DoAndReturn(
				func(_ *gofr.Context, orgID int64, hostname, token string) (*models.Domain, error) {
					assert.Regexp(t, tokenRe, token, "token must be 32 random base62 chars")

					d := pendingDomain(21, hostname)
					d.VerificationToken = token

					return d, nil
				})

			got, err := svc.Create(ctx, 3, 7, tc.input)

			require.NoError(t, err)
			assert.Equal(t, tc.want, got.Hostname)
			assert.Equal(t, models.DomainStatusPending, got.Status)
			assert.Equal(t, "_ogtr-verify."+tc.want, got.TXTRecordName)
			assert.Equal(t, got.VerificationToken, got.TXTRecordValue)
		})
	}
}

func TestDomainService_Create_RejectsInvalidHostnames(t *testing.T) {
	tests := []struct {
		desc  string
		input string
	}{
		{"empty", "   "},
		{"scheme", "https://links.example.com"},
		{"path", "links.example.com/path"},
		{"port", "links.example.com:443"},
		{"ipv4 literal", "192.168.1.10"},
		{"ipv6 literal", "[2001:db8::1]"},
		{"bare ipv6", "2001:db8::1"},
		{"single label", "links"},
		{"leading hyphen label", "-bad.example.com"},
		{"underscore label", "foo_bar.example.com"},
		{"space inside", "links example.com"},
		{"empty label", "links..example.com"},
		{"the deployment short domain itself", "sho.rt"},
		{"the short domain uppercased", "SHO.RT"},
		{"a subdomain of the short domain", "go.sho.rt"},
		{"a deep subdomain of the short domain", "a.b.sho.rt"},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, m, ctx := newDomainService(t)
			stubOwner(m)

			_, err := svc.Create(ctx, 3, 7, tc.input)

			require.Error(t, err)
			assertStatus(t, err, http.StatusUnprocessableEntity)
		})
	}
}

func TestDomainService_Create_GlobalUniqueness(t *testing.T) {
	// Another org (or this one) already registered the hostname: 409, and
	// the message does not reveal who holds it.
	svc, m, ctx := newDomainService(t)
	stubOwner(m)

	taken := pendingDomain(5, "links.example.com")
	taken.OrgID = 99

	m.domains.EXPECT().GetByHostname(gomock.Any(), "links.example.com").Return(taken, nil)

	_, err := svc.Create(ctx, 3, 7, "links.example.com")

	require.Error(t, err)
	assertStatus(t, err, http.StatusConflict)
}

func TestDomainService_MutationsAreOwnerOnly(t *testing.T) {
	tests := []struct {
		desc string
		call func(svc *DomainService, ctx *gofr.Context) error
	}{
		{"create", func(svc *DomainService, ctx *gofr.Context) error {
			_, err := svc.Create(ctx, 3, 7, "links.example.com")

			return err
		}},
		{"verify", func(svc *DomainService, ctx *gofr.Context) error {
			_, err := svc.Verify(ctx, 3, 7, 21)

			return err
		}},
		{"set primary", func(svc *DomainService, ctx *gofr.Context) error {
			_, err := svc.SetPrimary(ctx, 3, 7, 21)

			return err
		}},
		{"delete", func(svc *DomainService, ctx *gofr.Context) error {
			return svc.Delete(ctx, 3, 7, 21)
		}},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, m, ctx := newDomainService(t)
			m.members.EXPECT().GetRole(gomock.Any(), int64(3), int64(7)).Return(models.RoleMember, nil)

			err := tc.call(svc, ctx)

			require.Error(t, err)
			assertStatus(t, err, http.StatusForbidden)
		})
	}
}

func TestDomainService_List_FillsTXTInstructions(t *testing.T) {
	// Listing needs no role check — members see the org's domains too.
	svc, m, ctx := newDomainService(t)
	m.domains.EXPECT().ListByOrg(gomock.Any(), int64(3)).
		Return([]models.Domain{*pendingDomain(21, "links.example.com")}, nil)

	got, err := svc.List(ctx, 3)

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "_ogtr-verify.links.example.com", got[0].TXTRecordName)
	assert.Equal(t, got[0].VerificationToken, got[0].TXTRecordValue)
}

func TestDomainService_Verify_Success(t *testing.T) {
	svc, m, ctx := newDomainService(t)
	stubOwner(m)

	domain := pendingDomain(21, "links.example.com")

	gomock.InOrder(
		m.domains.EXPECT().GetByID(gomock.Any(), int64(3), int64(21)).Return(domain, nil),
		m.domains.EXPECT().GetByID(gomock.Any(), int64(3), int64(21)).
			Return(verifiedDomain(21, "links.example.com"), nil),
	)

	m.dns.EXPECT().LookupTXT(gomock.Any(), "_ogtr-verify.links.example.com").DoAndReturn(
		func(lookupCtx context.Context, _ string) ([]string, error) {
			_, hasDeadline := lookupCtx.Deadline()
			assert.True(t, hasDeadline, "the TXT lookup must be time-bounded")

			// Unrelated values around the matching one; surrounding
			// whitespace is tolerated.
			return []string{"v=spf1 -all", " " + domain.VerificationToken + " "}, nil
		})
	m.domains.EXPECT().SetVerified(gomock.Any(), int64(3), int64(21)).Return(nil)

	got, err := svc.Verify(ctx, 3, 7, 21)

	require.NoError(t, err)
	assert.Equal(t, models.DomainStatusVerified, got.Status)
}

func TestDomainService_Verify_DNSFailures(t *testing.T) {
	tests := []struct {
		desc    string
		records []string
		err     error
	}{
		{"record not found", nil, &net.DNSError{Err: "no such host", IsNotFound: true}},
		{"lookup timeout", nil, context.DeadlineExceeded},
		{"value mismatch", []string{"something-else"}, nil},
		{"no TXT values at all", []string{}, nil},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, m, ctx := newDomainService(t)
			stubOwner(m)

			m.domains.EXPECT().GetByID(gomock.Any(), int64(3), int64(21)).
				Return(pendingDomain(21, "links.example.com"), nil)
			m.dns.EXPECT().LookupTXT(gomock.Any(), "_ogtr-verify.links.example.com").
				Return(tc.records, tc.err)

			// SetVerified must never be called (no expectation set).
			_, err := svc.Verify(ctx, 3, 7, 21)

			require.Error(t, err)
			assertStatus(t, err, http.StatusConflict)
		})
	}
}

func TestDomainService_Verify_AlreadyVerifiedIsIdempotent(t *testing.T) {
	// No DNS lookup, no write — the domain comes back as-is.
	svc, m, ctx := newDomainService(t)
	stubOwner(m)
	m.domains.EXPECT().GetByID(gomock.Any(), int64(3), int64(21)).
		Return(verifiedDomain(21, "links.example.com"), nil)

	got, err := svc.Verify(ctx, 3, 7, 21)

	require.NoError(t, err)
	assert.Equal(t, models.DomainStatusVerified, got.Status)
}

func TestDomainService_Verify_DisabledDomain(t *testing.T) {
	svc, m, ctx := newDomainService(t)
	stubOwner(m)

	domain := pendingDomain(21, "links.example.com")
	domain.Status = models.DomainStatusDisabled

	m.domains.EXPECT().GetByID(gomock.Any(), int64(3), int64(21)).Return(domain, nil)

	_, err := svc.Verify(ctx, 3, 7, 21)

	require.Error(t, err)
	assertStatus(t, err, http.StatusConflict)
}

func TestDomainService_Verify_ShortDomainRecheckedAtVerifyTime(t *testing.T) {
	// SHORT_DOMAIN can change between deployments: a stored hostname that is
	// now under the deployment's own domain must not verify (422), without
	// any DNS traffic.
	svc, m, ctx := newDomainService(t)
	stubOwner(m)
	m.domains.EXPECT().GetByID(gomock.Any(), int64(3), int64(21)).
		Return(pendingDomain(21, "go.sho.rt"), nil)

	_, err := svc.Verify(ctx, 3, 7, 21)

	require.Error(t, err)
	assertStatus(t, err, http.StatusUnprocessableEntity)
}

func TestDomainService_Verify_UnknownID(t *testing.T) {
	svc, m, ctx := newDomainService(t)
	stubOwner(m)
	m.domains.EXPECT().GetByID(gomock.Any(), int64(3), int64(21)).Return(nil, nil)

	_, err := svc.Verify(ctx, 3, 7, 21)

	require.Error(t, err)
	assertStatus(t, err, http.StatusNotFound)
}

func TestDomainService_SetPrimary(t *testing.T) {
	t.Run("verified domain becomes primary", func(t *testing.T) {
		svc, m, ctx := newDomainService(t)
		stubOwner(m)

		primary := verifiedDomain(21, "links.example.com")
		primary.IsPrimary = true

		gomock.InOrder(
			m.domains.EXPECT().GetByID(gomock.Any(), int64(3), int64(21)).
				Return(verifiedDomain(21, "links.example.com"), nil),
			m.domains.EXPECT().SetPrimary(gomock.Any(), int64(3), int64(21)).Return(true, nil),
			m.domains.EXPECT().GetByID(gomock.Any(), int64(3), int64(21)).Return(primary, nil),
		)

		got, err := svc.SetPrimary(ctx, 3, 7, 21)

		require.NoError(t, err)
		assert.True(t, got.IsPrimary)
	})

	t.Run("pending domain cannot be primary", func(t *testing.T) {
		svc, m, ctx := newDomainService(t)
		stubOwner(m)
		m.domains.EXPECT().GetByID(gomock.Any(), int64(3), int64(21)).
			Return(pendingDomain(21, "links.example.com"), nil)

		_, err := svc.SetPrimary(ctx, 3, 7, 21)

		require.Error(t, err)
		assertStatus(t, err, http.StatusUnprocessableEntity)
	})

	t.Run("concurrent unverify rolls the swap back", func(t *testing.T) {
		// The store's transactional swap matched no VERIFIED row (false):
		// the previous primary survives and the caller gets 422.
		svc, m, ctx := newDomainService(t)
		stubOwner(m)
		m.domains.EXPECT().GetByID(gomock.Any(), int64(3), int64(21)).
			Return(verifiedDomain(21, "links.example.com"), nil)
		m.domains.EXPECT().SetPrimary(gomock.Any(), int64(3), int64(21)).Return(false, nil)

		_, err := svc.SetPrimary(ctx, 3, 7, 21)

		require.Error(t, err)
		assertStatus(t, err, http.StatusUnprocessableEntity)
	})
}

func TestDomainService_Delete(t *testing.T) {
	t.Run("owner deletes", func(t *testing.T) {
		svc, m, ctx := newDomainService(t)
		stubOwner(m)
		m.domains.EXPECT().Delete(gomock.Any(), int64(3), int64(21)).Return(true, nil)

		require.NoError(t, svc.Delete(ctx, 3, 7, 21))
	})

	t.Run("unknown or cross-org id is 404", func(t *testing.T) {
		svc, m, ctx := newDomainService(t)
		stubOwner(m)
		m.domains.EXPECT().Delete(gomock.Any(), int64(3), int64(21)).Return(false, nil)

		err := svc.Delete(ctx, 3, 7, 21)

		require.Error(t, err)
		assertStatus(t, err, http.StatusNotFound)
	})
}

func TestNormalizeHost(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"links.example.com", "links.example.com"},
		{"LINKS.example.com:443", "links.example.com"},
		{"localhost:5810", "localhost"},
		{"[::1]:5810", "::1"},
		{"links.example.com.", "links.example.com"},
		{"", ""},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.want, NormalizeHost(tc.in), "NormalizeHost(%q)", tc.in)
	}
}

func TestIsDeploymentHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"sho.rt", true},
		{"SHO.RT:443", true},
		{"localhost", true},
		{"localhost:5810", true},
		{"127.0.0.1:5810", true},
		{"[::1]:5810", true},
		{"", true}, // no Host known: behave as the deployment domain
		{"links.example.com", false},
		{"go.sho.rt", false}, // subdomains are NOT the deployment host
	}

	for _, tc := range tests {
		assert.Equal(t, tc.want, IsDeploymentHost(tc.host, "sho.rt:5810"), "IsDeploymentHost(%q)", tc.host)
	}
}
