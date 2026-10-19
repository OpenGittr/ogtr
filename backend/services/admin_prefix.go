package services

import (
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/models"
)

// maxDecisionReasonLength bounds the operator's reject/revoke reason
// (brand_prefixes.decision_reason).
const maxDecisionReasonLength = 500

// AdminPrefixesPage is one page of the operator's brand-prefix queue.
type AdminPrefixesPage struct {
	Prefixes []models.AdminBrandPrefix `json:"prefixes"`
	Total    int64                     `json:"total"`
}

// Prefixes returns one page (25/page) of brand-prefix requests in the given
// status (default PENDING — the review queue), oldest first.
func (s *AdminService) Prefixes(ctx *gofr.Context, status string, page int) (*AdminPrefixesPage, error) {
	status = strings.ToUpper(strings.TrimSpace(status))
	if status == "" {
		status = models.BrandPrefixStatusPending
	}

	switch status {
	case models.BrandPrefixStatusPending, models.BrandPrefixStatusApproved,
		models.BrandPrefixStatusRejected, models.BrandPrefixStatusRevoked:
	default:
		return nil, apierrors.Unprocessable("status must be PENDING, APPROVED, REJECTED or REVOKED")
	}

	prefixes, err := s.store.ListPrefixes(ctx, status, adminPageSize, pageOffset(page))
	if err != nil {
		return nil, err
	}

	total, err := s.store.CountPrefixes(ctx, status)
	if err != nil {
		return nil, err
	}

	return &AdminPrefixesPage{Prefixes: prefixes, Total: total}, nil
}

// ApprovePrefix moves a PENDING prefix to APPROVED: the org's links start
// resolving under it.
func (s *AdminService) ApprovePrefix(ctx *gofr.Context, id int64) (*models.AdminBrandPrefix, error) {
	return s.decidePrefix(ctx, id, models.BrandPrefixStatusPending, models.BrandPrefixStatusApproved, "")
}

// RejectPrefix moves a PENDING prefix to REJECTED (reason required). A
// rejected prefix holds nothing — any org may request the value again.
func (s *AdminService) RejectPrefix(ctx *gofr.Context, id int64, reason string) (*models.AdminBrandPrefix, error) {
	return s.decidePrefix(ctx, id, models.BrandPrefixStatusPending, models.BrandPrefixStatusRejected, reason)
}

// RevokePrefix moves an APPROVED prefix to REVOKED (reason required):
// /<prefix>/<code> stops resolving at once, the org's links keep working at
// /<code>, and the value stays held until released.
func (s *AdminService) RevokePrefix(ctx *gofr.Context, id int64, reason string) (*models.AdminBrandPrefix, error) {
	return s.decidePrefix(ctx, id, models.BrandPrefixStatusApproved, models.BrandPrefixStatusRevoked, reason)
}

// ReleasePrefix deletes a REVOKED prefix so its value is claimable again.
func (s *AdminService) ReleasePrefix(ctx *gofr.Context, id int64) error {
	p, err := s.getPrefix(ctx, id)
	if err != nil {
		return err
	}

	released, err := s.store.ReleasePrefix(ctx, id)
	if err != nil {
		return err
	}

	if !released {
		return apierrors.Conflict("only a revoked prefix can be released (this one is " + p.Status + ")")
	}

	ctx.Logger.Infof("admin: brand prefix %d (%s) of org %d released", id, p.Prefix, p.OrgID)

	return nil
}

// decidePrefix applies one operator transition. The store write is
// conditional on `from`, so a stale or concurrent decision is a 409 and
// changes nothing. Each decision is logged with its reason (audit-light, like
// link disable) and mailed to the org's owners (best-effort).
func (s *AdminService) decidePrefix(ctx *gofr.Context, id int64, from, to, reason string) (*models.AdminBrandPrefix, error) {
	reason = strings.TrimSpace(reason)

	if to != models.BrandPrefixStatusApproved && reason == "" {
		return nil, apierrors.Unprocessable("a reason is required")
	}

	if len(reason) > maxDecisionReasonLength {
		return nil, apierrors.Unprocessable("reason must be at most 500 characters")
	}

	p, err := s.getPrefix(ctx, id)
	if err != nil {
		return nil, err
	}

	var reasonArg *string
	if reason != "" {
		reasonArg = &reason
	}

	changed, err := s.store.DecidePrefix(ctx, id, from, to, reasonArg)
	if err != nil {
		return nil, err
	}

	if !changed {
		return nil, apierrors.Conflict("this prefix is " + p.Status + ", not " + from)
	}

	logReason := reason
	if logReason == "" {
		logReason = "none given"
	}

	ctx.Logger.Infof("admin: brand prefix %d (%s) of org %d %s → %s (reason: %s)",
		id, p.Prefix, p.OrgID, from, to, logReason)

	s.notifyOwners(ctx, p, to, reason)

	return s.getPrefix(ctx, id)
}

// notifyOwners mails the decision to every OWNER of the prefix's org. Only
// when mail is configured is the owner list looked up; a lookup failure is
// logged and never fails the decision.
func (s *AdminService) notifyOwners(ctx *gofr.Context, p *models.AdminBrandPrefix, status, reason string) {
	if !s.mailer.Enabled() {
		return
	}

	users, err := s.store.OrgUsers(ctx, p.OrgID)
	if err != nil {
		ctx.Logger.Errorf("admin: owner lookup for org %d failed (no decision email): %v", p.OrgID, err)

		return
	}

	for i := range users {
		if users[i].Role == models.RoleOwner {
			s.mailer.SendPrefixDecision(users[i].Email, p.Prefix, status, reason)
		}
	}
}

func (s *AdminService) getPrefix(ctx *gofr.Context, id int64) (*models.AdminBrandPrefix, error) {
	p, err := s.store.GetPrefix(ctx, id)
	if err != nil {
		return nil, err
	}

	if p == nil {
		return nil, apierrors.NotFound("prefix not found")
	}

	return p, nil
}
