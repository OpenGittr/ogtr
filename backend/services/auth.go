package services

import (
	"errors"
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/auth"
	"github.com/opengittr/ogtr/backend/email"
	"github.com/opengittr/ogtr/backend/limits"
	"github.com/opengittr/ogtr/backend/models"
)

// AuthResult is what a login (or org switch) hands back to the SPA. An empty
// Orgs slice with ActiveOrgID 0 signals the valid "no org yet" state.
type AuthResult struct {
	models.TokenPair
	User        *models.User           `json:"user"`
	Orgs        []models.OrgMembership `json:"orgs"`
	ActiveOrgID int64                  `json:"active_org_id"`
}

// MeResult is the GET /me payload.
type MeResult struct {
	User        *models.User           `json:"user"`
	Orgs        []models.OrgMembership `json:"orgs"`
	ActiveOrgID int64                  `json:"active_org_id"`
	Role        string                 `json:"role,omitempty"`
}

// AuthService implements login, refresh, me and org switching.
type AuthService struct {
	providers map[string]auth.IdentityProvider
	tokens    TokenIssuer
	users     UserStore
	orgs      OrgStore
	members   MemberStore
	invites   InviteStore
	policy    limits.Policy
	mailer    *email.Sender
}

// NewAuthService wires an AuthService. providers maps enabled provider names
// (auth.ProviderGoogle, auth.ProviderMicrosoft, auth.ProviderDev) to their
// IdentityProvider — only
// enabled providers are present, so a login via a disabled provider fails
// with 404 even if a route somehow reaches the service. policy bounds
// membership creation on the login path (invite acceptance, auto-join); wire
// limits.Unlimited{} unless the deployment supplies its own.
func NewAuthService(providers map[string]auth.IdentityProvider, tokens TokenIssuer,
	users UserStore, orgs OrgStore, members MemberStore, invites InviteStore,
	policy limits.Policy, mailer *email.Sender) *AuthService {
	return &AuthService{
		providers: providers, tokens: tokens, users: users,
		orgs: orgs, members: members, invites: invites, policy: policy, mailer: mailer,
	}
}

// Login exchanges a provider credential (Google ID token, dev email/name
// JSON) for ogtr session tokens: verify identity with the named
// provider, JIT-create the user, convert pending invites to memberships,
// auto-join by email domain when otherwise org-less, and scope the token
// pair to the org of an invite just accepted, else the user's first org (or
// none). The flow after Verify is identical for every provider.
func (s *AuthService) Login(ctx *gofr.Context, provider, credential string) (*AuthResult, error) {
	idp, ok := s.providers[provider]
	if !ok {
		return nil, apierrors.NotFound("this sign-in method is not enabled on this server")
	}

	identity, err := idp.Verify(ctx, credential)
	if err != nil {
		return nil, err
	}

	email := strings.ToLower(strings.TrimSpace(identity.Email))

	local, domain := splitEmail(email)
	if local == "" || domain == "" {
		return nil, apierrors.Unauthorized("identity provider returned an invalid email")
	}

	user, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		return nil, err
	}

	if user == nil {
		name := identity.Name
		if name == "" {
			name = local
		}

		user, err = s.users.Create(ctx, name, email)
		if err != nil {
			return nil, err
		}

		ctx.Logger.Infof("JIT-created user %d for %s", user.ID, email)

		// Welcome the new user (best-effort, async; no-op if mail unconfigured).
		s.mailer.SendWelcome(user.Email, user.Name)
	}

	s.touchLastActive(ctx, user.ID)

	joined, err := s.acceptPendingInvites(ctx, user.ID, email)
	if err != nil {
		return nil, err
	}

	orgs, err := s.members.ListOrgsForUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	if len(orgs) == 0 {
		orgs, err = s.autoJoinByDomain(ctx, user, domain)
		if err != nil {
			return nil, err
		}
	}

	return s.buildAuthResult(user, orgs, joined)
}

// touchLastActive records "seen now" on the two session-granting paths
// (login, refresh). The store throttles the write to at most one per user
// per hour; a failure is logged and swallowed — activity telemetry must
// never fail a sign-in.
func (s *AuthService) touchLastActive(ctx *gofr.Context, userID int64) {
	if err := s.users.TouchLastActive(ctx, userID); err != nil {
		ctx.Logger.Warnf("could not touch last_active_at for user %d: %v", userID, err)
	}
}

// joinAsMember is the single membership-creating choke point on the login
// path (invite acceptance and auto-join both go through it): it consults the
// deployment's limits.Policy (CanAddMember) and only then writes the
// membership. A denial must never fail a login — the user simply does not
// join (joined=false), the denial is logged, and a pending invite stays
// PENDING so it converts on a later login once the policy allows. Any other
// policy error is an internal failure and propagates.
func (s *AuthService) joinAsMember(ctx *gofr.Context, orgID, userID int64) (joined bool, err error) {
	if err := s.policy.CanAddMember(ctx, orgID); err != nil {
		var denial *limits.Denial
		if errors.As(err, &denial) {
			ctx.Logger.Infof("user %d not joined to org %d (policy): %s", userID, orgID, denial.Error())

			return false, nil
		}

		return false, err
	}

	if err := s.members.Add(ctx, orgID, userID, models.RoleMember); err != nil {
		// A concurrent request (login racing /me or refresh) may have added
		// the same membership first; that is a join, not a failure.
		if role, roleErr := s.members.GetRole(ctx, orgID, userID); roleErr == nil && role != "" {
			return true, nil
		}

		return false, err
	}

	return true, nil
}

// acceptPendingInvites converts the email's pending invites to memberships
// (via the joinAsMember choke point — a policy denial leaves the invite
// PENDING and the caller continues). It runs on login, refresh and /me, so
// a user who is already signed in picks up an invite without signing out.
// It returns the orgs joined by this call, oldest invite first.
func (s *AuthService) acceptPendingInvites(ctx *gofr.Context, userID int64, email string) ([]int64, error) {
	invites, err := s.invites.PendingForEmail(ctx, email)
	if err != nil {
		return nil, err
	}

	var joinedOrgs []int64

	for i := range invites {
		role, err := s.members.GetRole(ctx, invites[i].OrgID, userID)
		if err != nil {
			return nil, err
		}

		if role == "" {
			joined, err := s.joinAsMember(ctx, invites[i].OrgID, userID)
			if err != nil {
				return nil, err
			}

			if !joined {
				continue // invite stays PENDING; retried on a later call
			}

			joinedOrgs = append(joinedOrgs, invites[i].OrgID)
		}

		if err := s.invites.SetStatus(ctx, invites[i].ID, models.InviteStatusAccepted); err != nil {
			return nil, err
		}

		ctx.Logger.Infof("invite %d accepted: user %d joined org %d", invites[i].ID, userID, invites[i].OrgID)
	}

	return joinedOrgs, nil
}

// autoJoinByDomain joins an org-less user to the first org whose
// auto_join_domain matches their email domain, as MEMBER (via the
// joinAsMember choke point — a policy denial leaves the user org-less and the
// login continues).
func (s *AuthService) autoJoinByDomain(ctx *gofr.Context, user *models.User, domain string) ([]models.OrgMembership, error) {
	org, err := s.orgs.GetByAutoJoinDomain(ctx, domain)
	if err != nil {
		return nil, err
	}

	if org == nil {
		return []models.OrgMembership{}, nil
	}

	joined, err := s.joinAsMember(ctx, org.ID, user.ID)
	if err != nil {
		return nil, err
	}

	if !joined {
		return []models.OrgMembership{}, nil
	}

	ctx.Logger.Infof("user %d auto-joined org %d via domain %s", user.ID, org.ID, domain)

	return []models.OrgMembership{{OrgID: org.ID, Name: org.Name, Slug: org.Slug, Role: models.RoleMember}}, nil
}

// buildAuthResult scopes the pair to the most recently joined org when this
// login accepted an invite (so an invitee who already has an org lands in
// the one they were invited to), else to the user's first org.
func (s *AuthService) buildAuthResult(user *models.User, orgs []models.OrgMembership,
	joined []int64) (*AuthResult, error) {
	var (
		activeOrgID int64
		role        string
	)

	if len(orgs) > 0 {
		activeOrgID, role = orgs[0].OrgID, orgs[0].Role
	}

	if len(joined) > 0 {
		for _, o := range orgs {
			if o.OrgID == joined[len(joined)-1] {
				activeOrgID, role = o.OrgID, o.Role
			}
		}
	}

	pair, err := s.tokens.IssuePair(user.ID, activeOrgID, role)
	if err != nil {
		return nil, err
	}

	return &AuthResult{TokenPair: pair, User: user, Orgs: orgs, ActiveOrgID: activeOrgID}, nil
}

// Refresh validates a refresh token and issues a fresh pair. Membership is
// re-checked so a revoked member does not keep an org-scoped session; if the
// membership is gone the new pair is org-less. Pending invites are accepted
// here too; an org-less session is scoped to the org it just joined.
func (s *AuthService) Refresh(ctx *gofr.Context, refreshToken string) (models.TokenPair, error) {
	claims, err := s.tokens.Parse(refreshToken, auth.TokenTypeRefresh)
	if err != nil {
		return models.TokenPair{}, err
	}

	user, err := s.users.GetByID(ctx, claims.UserID)
	if err != nil {
		return models.TokenPair{}, err
	}

	if user == nil || user.Status != models.UserStatusEnabled {
		return models.TokenPair{}, apierrors.Unauthorized("account is not active")
	}

	s.touchLastActive(ctx, user.ID)

	joined, err := s.acceptPendingInvites(ctx, user.ID, user.Email)
	if err != nil {
		return models.TokenPair{}, err
	}

	var (
		orgID int64
		role  string
	)

	if claims.OrgID > 0 {
		role, err = s.members.GetRole(ctx, claims.OrgID, user.ID)
		if err != nil {
			return models.TokenPair{}, err
		}

		if role != "" {
			orgID = claims.OrgID
		}
	}

	if orgID == 0 && len(joined) > 0 {
		orgID, role = joined[len(joined)-1], models.RoleMember
	}

	return s.tokens.IssuePair(user.ID, orgID, role)
}

// Me returns the current user with all their org memberships, accepting any
// pending invites first so a newly joined org shows up without a re-login.
// The session's active org is unchanged; the SPA switches to a joined org.
func (s *AuthService) Me(ctx *gofr.Context, claims *auth.SessionClaims) (*MeResult, error) {
	user, err := s.users.GetByID(ctx, claims.UserID)
	if err != nil {
		return nil, err
	}

	if user == nil {
		return nil, apierrors.Unauthorized("account no longer exists")
	}

	if _, err := s.acceptPendingInvites(ctx, user.ID, user.Email); err != nil {
		return nil, err
	}

	orgs, err := s.members.ListOrgsForUser(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	return &MeResult{User: user, Orgs: orgs, ActiveOrgID: claims.OrgID, Role: claims.Role}, nil
}

// splitEmail returns the local part and domain of an email address; empty
// strings when the address is not of the form local@domain.
func splitEmail(email string) (local, domain string) {
	at := strings.LastIndex(email, "@")
	if at <= 0 || at == len(email)-1 {
		return "", ""
	}

	return email[:at], email[at+1:]
}

// SwitchOrg issues a token pair re-scoped to the requested org after
// validating the user's membership in it.
func (s *AuthService) SwitchOrg(ctx *gofr.Context, userID, orgID int64) (models.TokenPair, error) {
	role, err := s.members.GetRole(ctx, orgID, userID)
	if err != nil {
		return models.TokenPair{}, err
	}

	if role == "" {
		return models.TokenPair{}, apierrors.Forbidden("you are not a member of this org")
	}

	return s.tokens.IssuePair(userID, orgID, role)
}
