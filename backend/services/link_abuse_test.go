package services

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"

	"github.com/opengittr/ogtr/backend/models"
	"github.com/opengittr/ogtr/backend/scanner"
)

// newScannedLinkService builds a LinkService whose URLScanner is a mock,
// for the creation/edit enforcement tests. abuseContact "" by default;
// variants pass one.
func newScannedLinkService(t *testing.T, abuseContact string) (
	*LinkService, *MockLinkStore, *MockURLScanner, *gofr.Context) {
	t.Helper()

	ctrl := gomock.NewController(t)
	links := NewMockLinkStore(ctrl)
	members := NewMockMemberStore(ctrl)
	domains := NewMockDomainStore(ctrl)
	urlScanner := NewMockURLScanner(ctrl)

	domains.EXPECT().PrimaryVerifiedHostname(gomock.Any(), gomock.Any()).Return("", nil).AnyTimes()

	mockContainer, _ := container.NewMockContainer(t)
	ctx := &gofr.Context{Context: context.Background(), Container: mockContainer}

	svc := NewLinkService(links, members, domains, urlScanner, nil, "http", "sho.rt", abuseContact)

	return svc, links, urlScanner, ctx
}

const flaggedMsg = "This destination was flagged by security checks and can't be shortened."

func TestLinkService_Shorten_FlaggedDestinationIs422(t *testing.T) {
	tests := []struct {
		desc         string
		abuseContact string
		wantMsg      string
	}{
		{
			desc:    "without ABUSE_CONTACT: coarse message only",
			wantMsg: flaggedMsg,
		},
		{
			desc:         "with ABUSE_CONTACT: appeal line appended",
			abuseContact: "abuse@sho.rt",
			wantMsg:      flaggedMsg + " If you believe this is a mistake, contact abuse@sho.rt.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, _, urlScanner, ctx := newScannedLinkService(t, tc.abuseContact)

			// The scanner sees the normalized destination, pre-UTM. No store
			// calls happen: flagged wins before dedupe and create.
			urlScanner.EXPECT().Scan(gomock.Any(), "https://evil.example/x").
				Return(scanner.Flag(scanner.CategoryMalware), nil)

			_, err := svc.Shorten(ctx, 3, 7, ShortenInput{URL: "evil.example/x", UTMSource: "s"})

			require.Error(t, err)
			assertStatus(t, err, http.StatusUnprocessableEntity)
			assert.Equal(t, tc.wantMsg, err.Error(), "message is coarse — no list/rule/category leaks")
		})
	}
}

func TestLinkService_Shorten_CleanDestinationPasses(t *testing.T) {
	svc, links, urlScanner, ctx := newScannedLinkService(t, "abuse@sho.rt")

	urlScanner.EXPECT().Scan(gomock.Any(), "https://clean.example/x").Return(scanner.Allow(), nil)
	links.EXPECT().FindByDestination(gomock.Any(), int64(3), int64(7), "https://clean.example/x").Return(nil, nil)
	links.EXPECT().CodeExists(gomock.Any(), gomock.Any()).Return(false, nil)
	links.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, l *models.Link) (*models.Link, error) {
			l.ID = 9

			return l, nil
		})

	got, err := svc.Shorten(ctx, 3, 7, ShortenInput{URL: "clean.example/x"})

	require.NoError(t, err)
	assert.Equal(t, "https://clean.example/x", got.DestinationURL)
}

func TestLinkService_Shorten_ScannerErrorFailsOpen(t *testing.T) {
	svc, links, urlScanner, ctx := newScannedLinkService(t, "")

	urlScanner.EXPECT().Scan(gomock.Any(), gomock.Any()).Return(scanner.Verdict{}, assert.AnError)
	links.EXPECT().FindByDestination(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
	links.EXPECT().CodeExists(gomock.Any(), gomock.Any()).Return(false, nil)
	links.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, l *models.Link) (*models.Link, error) { return l, nil })

	_, err := svc.Shorten(ctx, 3, 7, ShortenInput{URL: "x.example"})

	require.NoError(t, err, "a scan-layer failure must not block creation (pipeline floor never errors)")
}

func TestLinkService_UpdateDestination_FlaggedIs422(t *testing.T) {
	svc, links, urlScanner, ctx := newScannedLinkService(t, "abuse@sho.rt")

	links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
		Return(publicLink(9, "abc1234", "https://old.example"), nil)
	urlScanner.EXPECT().Scan(gomock.Any(), "https://evil.example/x").
		Return(scanner.Flag(scanner.CategoryPhishing), nil)

	// Creator edits their own link (no role lookup); no update or audit row
	// is ever written for a flagged destination.
	_, err := svc.UpdateDestination(ctx, 3, 7, 9, EditInput{URL: "evil.example/x"})

	require.Error(t, err)
	assertStatus(t, err, http.StatusUnprocessableEntity)
	assert.Contains(t, err.Error(), flaggedMsg)
	assert.Contains(t, err.Error(), "abuse@sho.rt")
}

func TestLinkService_UniqueCode_RetriesReservedWords(t *testing.T) {
	svc, links, _, ctx := newScannedLinkService(t, "")

	// Stub the generator: first draw is "pricing" — 7 chars of base62, a
	// perfectly legal generated-code shape AND a reserved word. It must be
	// discarded WITHOUT ever reaching CodeExists; the second draw goes
	// through normally.
	draws := []string{"pricing", "abc1234"}
	randomCodeFn = func(int) (string, error) {
		next := draws[0]
		draws = draws[1:]

		return next, nil
	}

	t.Cleanup(func() { randomCodeFn = randomCode })

	links.EXPECT().CodeExists(gomock.Any(), "abc1234").Return(false, nil)

	code, err := svc.uniqueCode(ctx)

	require.NoError(t, err)
	assert.Equal(t, "abc1234", code)
	assert.Empty(t, draws, "both draws consumed — the reserved word cost one retry")
}

func TestLinkService_SetAlias_ReservedScopeSplit(t *testing.T) {
	tests := []struct {
		desc        string
		alias       string
		hasVerified bool // org owns a VERIFIED custom domain
		wantStatus  int  // 0 = allowed
	}{
		{desc: "brand word reserved on shared domain", alias: "pricing", hasVerified: false,
			wantStatus: http.StatusUnprocessableEntity},
		{desc: "auth word reserved on shared domain", alias: "login", hasVerified: false,
			wantStatus: http.StatusUnprocessableEntity},
		{desc: "brand word allowed for custom-domain org", alias: "pricing", hasVerified: true},
		{desc: "auth word allowed for custom-domain org", alias: "login", hasVerified: true},
		{desc: "functional word reserved even for custom-domain org", alias: "api",
			hasVerified: true, wantStatus: http.StatusUnprocessableEntity},
		{desc: "functional word reserved on shared domain", alias: "metrics",
			wantStatus: http.StatusUnprocessableEntity},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			links := NewMockLinkStore(ctrl)
			domains := NewMockDomainStore(ctrl)

			mockContainer, _ := container.NewMockContainer(t)
			ctx := &gofr.Context{Context: context.Background(), Container: mockContainer}

			svc := NewLinkService(links, NewMockMemberStore(ctrl), domains, nil, nil, "http", "sho.rt", "")

			domains.EXPECT().HasVerified(gomock.Any(), int64(3)).Return(tc.hasVerified, nil)

			if tc.wantStatus == 0 {
				// Allowed aliases proceed into the normal set-alias flow.
				domains.EXPECT().PrimaryVerifiedHostname(gomock.Any(), int64(3)).Return("", nil).AnyTimes()
				links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
					Return(publicLink(9, "abc1234", "https://x.co"), nil).AnyTimes()
				links.EXPECT().CodeExists(gomock.Any(), tc.alias).Return(false, nil)
				links.EXPECT().UpdateCode(gomock.Any(), int64(3), int64(9), tc.alias).Return(nil)
			}

			_, err := svc.SetAlias(ctx, 3, 7, 9, tc.alias)

			if tc.wantStatus == 0 {
				require.NoError(t, err)

				return
			}

			require.Error(t, err)
			assertStatus(t, err, tc.wantStatus)
			assert.Equal(t, "this alias is reserved", err.Error())
		})
	}
}

func TestLinkService_SetAlias_ConfiguredReservedWord(t *testing.T) {
	ctrl := gomock.NewController(t)
	links := NewMockLinkStore(ctrl)
	domains := NewMockDomainStore(ctrl)

	mockContainer, _ := container.NewMockContainer(t)
	ctx := &gofr.Context{Context: context.Background(), Container: mockContainer}

	svc := NewLinkService(links, NewMockMemberStore(ctrl), domains, nil,
		NewReservedAliases([]string{"campaign-x"}), "http", "sho.rt", "")

	// RESERVED_ALIASES additions bind in both scopes.
	for _, hasVerified := range []bool{false, true} {
		domains.EXPECT().HasVerified(gomock.Any(), int64(3)).Return(hasVerified, nil)

		_, err := svc.SetAlias(ctx, 3, 7, 9, "campaign-x")

		require.Error(t, err)
		assertStatus(t, err, http.StatusUnprocessableEntity)
	}
}

func TestLinkService_SetAlias_DomainLookupFailureFallsBackStrict(t *testing.T) {
	ctrl := gomock.NewController(t)
	links := NewMockLinkStore(ctrl)
	domains := NewMockDomainStore(ctrl)

	mockContainer, _ := container.NewMockContainer(t)
	ctx := &gofr.Context{Context: context.Background(), Container: mockContainer}

	svc := NewLinkService(links, NewMockMemberStore(ctrl), domains, nil, nil, "http", "sho.rt", "")

	domains.EXPECT().HasVerified(gomock.Any(), int64(3)).Return(false, assert.AnError)

	// On a failed lookup the STRICT list applies — never the permissive one.
	_, err := svc.SetAlias(ctx, 3, 7, 9, "pricing")

	require.Error(t, err)
	assertStatus(t, err, http.StatusUnprocessableEntity)
}
