package services

import (
	"regexp"
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/models"
)

const (
	// maxPrefixesPerOrg caps an org's held (non-rejected) prefixes — an abuse
	// guard against squatting, not a plan limit (limits.Policy is
	// deliberately not extended for prefixes).
	maxPrefixesPerOrg = 20

	minEntityNameLength = 2
	maxEntityNameLength = 255
)

var (
	// prefixRe is the brand-prefix format (FEATURES.md §1.7): DLT headers are
	// alphanumeric, so no '-' or '_'.
	prefixRe = regexp.MustCompile(`^[A-Za-z0-9]{3,20}$`)

	// entityIDRe is a DLT Principal Entity ID: numeric.
	entityIDRe = regexp.MustCompile(`^[0-9]{1,32}$`)
)

// BrandPrefixInput is the POST /api/v1/org/prefixes payload: the prefix plus
// the sender evidence the operator reviews.
type BrandPrefixInput struct {
	Prefix     string `json:"prefix"`
	Registry   string `json:"registry"`
	EntityID   string `json:"entity_id"`
	Header     string `json:"header"`
	EntityName string `json:"entity_name"`
}

// BrandPrefixService implements per-org brand prefixes (FEATURES.md §1.7):
// an org requests https://<SHORT_DOMAIN>/<prefix>/<code> with DLT sender
// evidence, an operator approves it (admin service), and the org's links
// then resolve — and can display — under it. Operator decisions live in
// AdminService; this is the org-facing side.
type BrandPrefixService struct {
	prefixes    BrandPrefixStore
	links       LinkStore
	members     MemberStore
	reserved    *ReservedAliases
	autoApprove bool
}

// NewBrandPrefixService wires a BrandPrefixService. reserved is the same
// reserved-word checker the link service uses (prefixes always face the full
// set); autoApprove (BRAND_PREFIX_AUTO_APPROVE) skips operator review.
func NewBrandPrefixService(prefixes BrandPrefixStore, links LinkStore, members MemberStore,
	reserved *ReservedAliases, autoApprove bool) *BrandPrefixService {
	if reserved == nil {
		reserved = NewReservedAliases(nil)
	}

	return &BrandPrefixService{
		prefixes: prefixes, links: links, members: members,
		reserved: reserved, autoApprove: autoApprove,
	}
}

// Create files a prefix request (OWNER only): PENDING for operator review, or
// APPROVED straight away under auto-approval. The prefix must equal the DLT
// header, pass the full reserved set and the explicit-word filter, and not
// equal any existing short code; a value held by any org is a 409.
func (s *BrandPrefixService) Create(ctx *gofr.Context, orgID, actorID int64, in BrandPrefixInput) (*models.BrandPrefix, error) {
	if err := s.requireOwner(ctx, orgID, actorID); err != nil {
		return nil, err
	}

	p, err := validatePrefixInput(in)
	if err != nil {
		return nil, err
	}

	if s.reserved.IsReserved(p.Prefix, false) || containsExplicit(p.Prefix) {
		return nil, apierrors.Unprocessable("this prefix is reserved")
	}

	held, err := s.prefixes.CountHeldByOrg(ctx, orgID)
	if err != nil {
		return nil, err
	}

	if held >= maxPrefixesPerOrg {
		return nil, apierrors.Unprocessable("an organization can hold at most 20 brand prefixes")
	}

	existing, err := s.prefixes.GetByPrefix(ctx, p.Prefix)
	if err != nil {
		return nil, err
	}

	if existing != nil && existing.Status != models.BrandPrefixStatusRejected {
		// Same message whether this or another org holds it (the unique
		// index backstops races).
		return nil, apierrors.Conflict("this prefix is already taken")
	}

	// A prefix equal to a short code would make /<prefix> ambiguous. Codes
	// compare case-insensitively through links.code's collation
	// (ARCHITECTURE.md §3).
	taken, err := s.links.CodeExists(ctx, p.Prefix)
	if err != nil {
		return nil, err
	}

	if taken {
		return nil, apierrors.Conflict("this prefix matches an existing short link — choose another or rename that link")
	}

	p.OrgID = orgID
	p.Status = models.BrandPrefixStatusPending

	if s.autoApprove {
		p.Status = models.BrandPrefixStatusApproved
	}

	created, err := s.prefixes.Create(ctx, p)
	if err != nil {
		return nil, err
	}

	ctx.Logger.Infof("brand prefix %d (%s) requested in org %d by user %d (status %s)",
		created.ID, created.Prefix, orgID, actorID, created.Status)

	return created, nil
}

// List returns the org's prefixes (any member may look).
func (s *BrandPrefixService) List(ctx *gofr.Context, orgID int64) ([]models.BrandPrefix, error) {
	return s.prefixes.ListByOrg(ctx, orgID)
}

// SetPrimary marks the prefix as the org's primary display prefix (OWNER
// only, APPROVED only — 422 otherwise).
func (s *BrandPrefixService) SetPrimary(ctx *gofr.Context, orgID, actorID, id int64) (*models.BrandPrefix, error) {
	if err := s.requireOwner(ctx, orgID, actorID); err != nil {
		return nil, err
	}

	p, err := s.get(ctx, orgID, id)
	if err != nil {
		return nil, err
	}

	if p.Status != models.BrandPrefixStatusApproved {
		return nil, apierrors.Unprocessable("only an approved prefix can be made primary")
	}

	swapped, err := s.prefixes.SetPrimary(ctx, orgID, id)
	if err != nil {
		return nil, err
	}

	if !swapped {
		// Revoked between our read and the swap; nothing changed.
		return nil, apierrors.Unprocessable("only an approved prefix can be made primary")
	}

	ctx.Logger.Infof("brand prefix %d (%s) set as primary in org %d by user %d", p.ID, p.Prefix, orgID, actorID)

	return s.get(ctx, orgID, id)
}

// Delete withdraws a request or removes a prefix (OWNER only). A REVOKED
// prefix is a 409: the value stays held until an operator releases it.
func (s *BrandPrefixService) Delete(ctx *gofr.Context, orgID, actorID, id int64) error {
	if err := s.requireOwner(ctx, orgID, actorID); err != nil {
		return err
	}

	p, err := s.get(ctx, orgID, id)
	if err != nil {
		return err
	}

	if p.Status == models.BrandPrefixStatusRevoked {
		return apierrors.Conflict("a revoked prefix can only be released by the operator")
	}

	deleted, err := s.prefixes.Delete(ctx, orgID, id)
	if err != nil {
		return err
	}

	if !deleted {
		return apierrors.Conflict("a revoked prefix can only be released by the operator")
	}

	ctx.Logger.Infof("brand prefix %d (%s) removed from org %d by user %d", p.ID, p.Prefix, orgID, actorID)

	return nil
}

// get fetches org-scoped; a cross-org or unknown id is 404.
func (s *BrandPrefixService) get(ctx *gofr.Context, orgID, id int64) (*models.BrandPrefix, error) {
	p, err := s.prefixes.GetByID(ctx, orgID, id)
	if err != nil {
		return nil, err
	}

	if p == nil {
		return nil, apierrors.NotFound("prefix not found")
	}

	return p, nil
}

// requireOwner checks the actor's role in the database (not the token claim).
func (s *BrandPrefixService) requireOwner(ctx *gofr.Context, orgID, actorID int64) error {
	role, err := s.members.GetRole(ctx, orgID, actorID)
	if err != nil {
		return err
	}

	if role != models.RoleOwner {
		return apierrors.Forbidden("only an org owner can manage brand prefixes")
	}

	return nil
}

// validatePrefixInput checks format and evidence (nothing is verified against
// DLT — there is no public API; the operator's review is the check). An
// empty registry defaults to IN-DLT, the only one accepted in v1.
func validatePrefixInput(in BrandPrefixInput) (*models.BrandPrefix, error) {
	p := &models.BrandPrefix{
		Prefix:     strings.TrimSpace(in.Prefix),
		Registry:   strings.ToUpper(strings.TrimSpace(in.Registry)),
		EntityID:   strings.TrimSpace(in.EntityID),
		Header:     strings.TrimSpace(in.Header),
		EntityName: strings.TrimSpace(in.EntityName),
	}

	if p.Registry == "" {
		p.Registry = models.BrandPrefixRegistryDLT
	}

	switch {
	case !prefixRe.MatchString(p.Prefix):
		return nil, apierrors.Unprocessable("prefix must be 3-20 letters or digits")
	case p.Registry != models.BrandPrefixRegistryDLT:
		return nil, apierrors.Unprocessable("registry must be IN-DLT")
	case !entityIDRe.MatchString(p.EntityID):
		return nil, apierrors.Unprocessable("entity_id must be your numeric DLT Principal Entity ID")
	case !strings.EqualFold(p.Header, p.Prefix):
		return nil, apierrors.Unprocessable("prefix must match your registered DLT header")
	case len(p.EntityName) < minEntityNameLength || len(p.EntityName) > maxEntityNameLength:
		return nil, apierrors.Unprocessable("entity_name must be 2-255 characters")
	}

	return p, nil
}
