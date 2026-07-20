package services

import (
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/auth"
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
}

// NewAuthService wires an AuthService. providers maps enabled provider names
// (auth.ProviderGoogle, auth.ProviderDev) to their IdentityProvider — only
// enabled providers are present, so a login via a disabled provider fails
// with 404 even if a route somehow reaches the service.
func NewAuthService(providers map[string]auth.IdentityProvider, tokens TokenIssuer,
	users UserStore, orgs OrgStore, members MemberStore, invites InviteStore) *AuthService {
	return &AuthService{providers: providers, tokens: tokens, users: users, orgs: orgs, members: members, invites: invites}
}

// Login exchanges a provider credential (Google ID token, dev email/name
// JSON) for ogtr session tokens: verify identity with the named
// provider, JIT-create the user, convert pending invites to memberships,
// auto-join by email domain when otherwise org-less, and scope the token
// pair to the user's first org (or none). The flow after Verify is identical
// for every provider.
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
	}

	if err := s.acceptPendingInvites(ctx, user, email); err != nil {
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

	return s.buildAuthResult(user, orgs)
}

// acceptPendingInvites converts the email's pending invites to memberships.
func (s *AuthService) acceptPendingInvites(ctx *gofr.Context, user *models.User, email string) error {
	invites, err := s.invites.PendingForEmail(ctx, email)
	if err != nil {
		return err
	}

	for i := range invites {
		role, err := s.members.GetRole(ctx, invites[i].OrgID, user.ID)
		if err != nil {
			return err
		}

		if role == "" {
			if err := s.members.Add(ctx, invites[i].OrgID, user.ID, models.RoleMember); err != nil {
				return err
			}
		}

		if err := s.invites.SetStatus(ctx, invites[i].ID, models.InviteStatusAccepted); err != nil {
			return err
		}

		ctx.Logger.Infof("invite %d accepted: user %d joined org %d", invites[i].ID, user.ID, invites[i].OrgID)
	}

	return nil
}

// autoJoinByDomain joins an org-less user to the first org whose
// auto_join_domain matches their email domain, as MEMBER.
func (s *AuthService) autoJoinByDomain(ctx *gofr.Context, user *models.User, domain string) ([]models.OrgMembership, error) {
	org, err := s.orgs.GetByAutoJoinDomain(ctx, domain)
	if err != nil {
		return nil, err
	}

	if org == nil {
		return []models.OrgMembership{}, nil
	}

	if err := s.members.Add(ctx, org.ID, user.ID, models.RoleMember); err != nil {
		return nil, err
	}

	ctx.Logger.Infof("user %d auto-joined org %d via domain %s", user.ID, org.ID, domain)

	return []models.OrgMembership{{OrgID: org.ID, Name: org.Name, Slug: org.Slug, Role: models.RoleMember}}, nil
}

func (s *AuthService) buildAuthResult(user *models.User, orgs []models.OrgMembership) (*AuthResult, error) {
	var (
		activeOrgID int64
		role        string
	)

	if len(orgs) > 0 {
		activeOrgID, role = orgs[0].OrgID, orgs[0].Role
	}

	pair, err := s.tokens.IssuePair(user.ID, activeOrgID, role)
	if err != nil {
		return nil, err
	}

	return &AuthResult{TokenPair: pair, User: user, Orgs: orgs, ActiveOrgID: activeOrgID}, nil
}

// Refresh validates a refresh token and issues a fresh pair. Membership is
// re-checked so a revoked member does not keep an org-scoped session; if the
// membership is gone the new pair is org-less.
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

	return s.tokens.IssuePair(user.ID, orgID, role)
}

// Me returns the current user with all their org memberships.
func (s *AuthService) Me(ctx *gofr.Context, claims *auth.SessionClaims) (*MeResult, error) {
	user, err := s.users.GetByID(ctx, claims.UserID)
	if err != nil {
		return nil, err
	}

	if user == nil {
		return nil, apierrors.Unauthorized("account no longer exists")
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
