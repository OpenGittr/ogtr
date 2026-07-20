package services

import (
	"errors"
	"fmt"
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/limits"
	"github.com/opengittr/ogtr/backend/models"
)

const maxSlugAttempts = 50

// OrgUpdate is a PATCH payload: nil fields are left unchanged; an empty
// AutoJoinDomain clears the domain.
type OrgUpdate struct {
	Name           *string `json:"name"`
	AutoJoinDomain *string `json:"auto_join_domain"`
}

// OrgService implements org management: create, update, members, invites.
type OrgService struct {
	orgs    OrgStore
	members MemberStore
	invites InviteStore
	users   UserStore
	policy  limits.Policy
}

// NewOrgService wires an OrgService. policy bounds org creation; wire
// limits.Unlimited{} unless the deployment supplies its own.
func NewOrgService(orgs OrgStore, members MemberStore, invites InviteStore, users UserStore, policy limits.Policy) *OrgService {
	return &OrgService{orgs: orgs, members: members, invites: invites, users: users, policy: policy}
}

// Create makes a new org with a unique slug derived from its name; the
// creator becomes OWNER. The deployment's limits.Policy is consulted after
// input validation and before any store access: a denial maps to 403 with
// code LIMIT_REACHED (message passed through), any other policy error is an
// internal failure.
func (s *OrgService) Create(ctx *gofr.Context, creatorID int64, name, autoJoinDomain string) (*models.Org, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, apierrors.Unprocessable("org name must not be empty")
	}

	domain, err := normalizeDomain(autoJoinDomain)
	if err != nil {
		return nil, err
	}

	if err := s.policy.CanCreateOrg(ctx, creatorID); err != nil {
		var denial *limits.Denial
		if errors.As(err, &denial) {
			return nil, apierrors.LimitReached(denial.Error())
		}

		return nil, err
	}

	slug, err := s.uniqueSlug(ctx, name)
	if err != nil {
		return nil, err
	}

	org, err := s.orgs.Create(ctx, name, slug, domain)
	if err != nil {
		return nil, err
	}

	if err := s.members.Add(ctx, org.ID, creatorID, models.RoleOwner); err != nil {
		return nil, err
	}

	ctx.Logger.Infof("org %d (%s) created by user %d", org.ID, org.Slug, creatorID)

	return org, nil
}

// Get returns the active org's details.
func (s *OrgService) Get(ctx *gofr.Context, orgID int64) (*models.Org, error) {
	org, err := s.orgs.GetByID(ctx, orgID)
	if err != nil {
		return nil, err
	}

	if org == nil {
		return nil, apierrors.NotFound("org not found")
	}

	return org, nil
}

// Update patches the org's name and/or auto_join_domain. OWNER only.
func (s *OrgService) Update(ctx *gofr.Context, orgID, actorID int64, patch OrgUpdate) (*models.Org, error) {
	if err := s.requireOwner(ctx, orgID, actorID); err != nil {
		return nil, err
	}

	org, err := s.Get(ctx, orgID)
	if err != nil {
		return nil, err
	}

	name := org.Name
	if patch.Name != nil {
		name = strings.TrimSpace(*patch.Name)
		if name == "" {
			return nil, apierrors.Unprocessable("org name must not be empty")
		}
	}

	domain := org.AutoJoinDomain
	if patch.AutoJoinDomain != nil {
		domain, err = normalizeDomain(*patch.AutoJoinDomain)
		if err != nil {
			return nil, err
		}
	}

	if err := s.orgs.Update(ctx, orgID, name, domain); err != nil {
		return nil, err
	}

	return s.Get(ctx, orgID)
}

// Members lists the org's members.
func (s *OrgService) Members(ctx *gofr.Context, orgID int64) ([]models.Member, error) {
	return s.members.ListMembers(ctx, orgID)
}

// RemoveMember removes a member. OWNER only; the last OWNER of an org can
// never be removed (which also stops an OWNER removing themselves when they
// are the last one).
func (s *OrgService) RemoveMember(ctx *gofr.Context, orgID, actorID, targetID int64) error {
	if err := s.requireOwner(ctx, orgID, actorID); err != nil {
		return err
	}

	targetRole, err := s.members.GetRole(ctx, orgID, targetID)
	if err != nil {
		return err
	}

	if targetRole == "" {
		return apierrors.NotFound("user is not a member of this org")
	}

	if targetRole == models.RoleOwner {
		owners, err := s.members.CountOwners(ctx, orgID)
		if err != nil {
			return err
		}

		if owners <= 1 {
			return apierrors.Unprocessable("cannot remove the last owner of the org")
		}
	}

	return s.members.Remove(ctx, orgID, targetID)
}

// CreateInvite invites an email into the org. OWNER only. The invite converts
// to membership automatically on the invitee's next login.
func (s *OrgService) CreateInvite(ctx *gofr.Context, orgID, actorID int64, email string) (*models.Invite, error) {
	if err := s.requireOwner(ctx, orgID, actorID); err != nil {
		return nil, err
	}

	email = strings.ToLower(strings.TrimSpace(email))

	if local, domain := splitEmail(email); local == "" || domain == "" {
		return nil, apierrors.Unprocessable("invalid email address")
	}

	user, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		return nil, err
	}

	if user != nil {
		role, err := s.members.GetRole(ctx, orgID, user.ID)
		if err != nil {
			return nil, err
		}

		if role != "" {
			return nil, apierrors.Conflict("user is already a member of this org")
		}
	}

	pending, err := s.invites.HasPending(ctx, orgID, email)
	if err != nil {
		return nil, err
	}

	if pending {
		return nil, apierrors.Conflict("a pending invite for this email already exists")
	}

	return s.invites.Create(ctx, orgID, email, actorID)
}

// ListInvites lists the org's pending invites.
func (s *OrgService) ListInvites(ctx *gofr.Context, orgID int64) ([]models.Invite, error) {
	return s.invites.ListPending(ctx, orgID)
}

// RevokeInvite revokes a pending invite. OWNER only.
func (s *OrgService) RevokeInvite(ctx *gofr.Context, orgID, actorID, inviteID int64) error {
	if err := s.requireOwner(ctx, orgID, actorID); err != nil {
		return err
	}

	invite, err := s.invites.GetByID(ctx, orgID, inviteID)
	if err != nil {
		return err
	}

	if invite == nil {
		return apierrors.NotFound("invite not found")
	}

	if invite.Status != models.InviteStatusPending {
		return apierrors.Conflict("invite is not pending")
	}

	return s.invites.SetStatus(ctx, inviteID, models.InviteStatusRevoked)
}

// requireOwner checks the actor's role in the database (not just the token)
// so a demoted or removed owner loses privileges immediately.
func (s *OrgService) requireOwner(ctx *gofr.Context, orgID, actorID int64) error {
	role, err := s.members.GetRole(ctx, orgID, actorID)
	if err != nil {
		return err
	}

	if role != models.RoleOwner {
		return apierrors.Forbidden("only an org owner can do this")
	}

	return nil
}

// uniqueSlug derives a URL-safe slug from the org name and suffixes it until
// it is unique.
func (s *OrgService) uniqueSlug(ctx *gofr.Context, name string) (string, error) {
	base := slugify(name)
	candidate := base

	for i := 0; i < maxSlugAttempts; i++ {
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d", base, i+1)
		}

		exists, err := s.orgs.SlugExists(ctx, candidate)
		if err != nil {
			return "", err
		}

		if !exists {
			return candidate, nil
		}
	}

	return "", apierrors.Conflict("could not derive a unique slug for this org name")
}

const maxSlugLen = 80

// slugify lowercases the name and collapses every non-alphanumeric run into a
// single hyphen.
func slugify(name string) string {
	var b strings.Builder

	lastHyphen := true // suppress leading hyphen

	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)

			lastHyphen = false
		case !lastHyphen:
			b.WriteByte('-')

			lastHyphen = true
		}
	}

	slug := strings.Trim(b.String(), "-")
	if len(slug) > maxSlugLen {
		slug = strings.Trim(slug[:maxSlugLen], "-")
	}

	if slug == "" {
		slug = "org"
	}

	return slug
}

// normalizeDomain validates an auto-join domain; empty input clears it (nil).
func normalizeDomain(domain string) (*string, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return nil, nil //nolint:nilnil // nil domain means "no auto-join", a valid value
	}

	if strings.ContainsAny(domain, "@/ \t") || !strings.Contains(domain, ".") {
		return nil, apierrors.Unprocessable("auto_join_domain must be a bare domain like example.com")
	}

	return &domain, nil
}
