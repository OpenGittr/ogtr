package services

import (
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/models"
)

// RuleInput is one target rule as submitted by the rule builder
// (POST /api/v1/links/{id}/rules and PUT /api/v1/rules/{ruleId}).
type RuleInput struct {
	TargetName string                `json:"target_name"`
	DeviceType *models.RuleCondition `json:"device_type"`
	Location   *models.RuleCondition `json:"location"`
	URL        string                `json:"url"`
}

// RuleService implements target-rule CRUD (FEATURES.md §3.1). All access is
// org-scoped and mirrors link-detail visibility: rules of another user's
// PRIVATE link are 404, never 403.
type RuleService struct {
	rules RuleStore
	links LinkStore
}

// NewRuleService wires a RuleService.
func NewRuleService(rules RuleStore, links LinkStore) *RuleService {
	return &RuleService{rules: rules, links: links}
}

// Create validates and appends rules to a link, in the submitted order —
// which is the resolution evaluation order (first match wins).
func (s *RuleService) Create(ctx *gofr.Context, orgID, viewerID, linkID int64, inputs []RuleInput) ([]models.Rule, error) {
	if len(inputs) == 0 {
		return nil, apierrors.Unprocessable("at least one rule is required")
	}

	normalized := make([]models.Rule, 0, len(inputs))

	for _, in := range inputs {
		rule, err := normalizeRule(in)
		if err != nil {
			return nil, err
		}

		rule.OrgID, rule.LinkID = orgID, linkID
		normalized = append(normalized, *rule)
	}

	if err := s.visibleLink(ctx, orgID, viewerID, linkID); err != nil {
		return nil, err
	}

	created := make([]models.Rule, 0, len(normalized))

	for i := range normalized {
		rule, err := s.rules.Create(ctx, &normalized[i])
		if err != nil {
			return nil, err
		}

		created = append(created, *rule)
	}

	ctx.Logger.Infof("%d target rule(s) added to link %d in org %d by user %d",
		len(created), linkID, orgID, viewerID)

	return created, nil
}

// List returns a link's rules in evaluation order.
func (s *RuleService) List(ctx *gofr.Context, orgID, viewerID, linkID int64) ([]models.Rule, error) {
	if err := s.visibleLink(ctx, orgID, viewerID, linkID); err != nil {
		return nil, err
	}

	return s.rules.ListByLink(ctx, orgID, linkID)
}

// Update replaces a rule's name, conditions and destination; its evaluation
// position is kept.
func (s *RuleService) Update(ctx *gofr.Context, orgID, viewerID, ruleID int64, in RuleInput) (*models.Rule, error) {
	normalized, err := normalizeRule(in)
	if err != nil {
		return nil, err
	}

	existing, err := s.visibleRule(ctx, orgID, viewerID, ruleID)
	if err != nil {
		return nil, err
	}

	normalized.ID, normalized.OrgID, normalized.LinkID = existing.ID, existing.OrgID, existing.LinkID

	return s.rules.Update(ctx, normalized)
}

// Delete removes a rule.
func (s *RuleService) Delete(ctx *gofr.Context, orgID, viewerID, ruleID int64) error {
	if _, err := s.visibleRule(ctx, orgID, viewerID, ruleID); err != nil {
		return err
	}

	return s.rules.Delete(ctx, orgID, ruleID)
}

// visibleLink 404s unless the link exists in the org and is visible to the
// viewer (PRIVATE links are creator-only, like link detail).
func (s *RuleService) visibleLink(ctx *gofr.Context, orgID, viewerID, linkID int64) error {
	link, err := s.links.GetByID(ctx, orgID, linkID)
	if err != nil {
		return err
	}

	if link == nil || !visibleTo(link, viewerID) {
		return apierrors.NotFound("link not found")
	}

	return nil
}

// visibleRule fetches a rule and applies the parent link's visibility.
func (s *RuleService) visibleRule(ctx *gofr.Context, orgID, viewerID, ruleID int64) (*models.Rule, error) {
	rule, err := s.rules.GetByID(ctx, orgID, ruleID)
	if err != nil {
		return nil, err
	}

	if rule == nil {
		return nil, apierrors.NotFound("rule not found")
	}

	if err := s.visibleLink(ctx, orgID, viewerID, rule.LinkID); err != nil {
		return nil, err
	}

	return rule, nil
}

// normalizeRule validates one rule: a parseable http(s) destination URL, at
// least one condition, and non-empty values on every present condition.
func normalizeRule(in RuleInput) (*models.Rule, error) {
	name := strings.TrimSpace(in.TargetName)
	if name == "" {
		return nil, apierrors.Unprocessable("rule needs a target_name")
	}

	dest, err := normalizeURL(in.URL)
	if err != nil {
		return nil, apierrors.Unprocessable("rule needs a valid destination url")
	}

	device, err := normalizeCondition("device_type", in.DeviceType)
	if err != nil {
		return nil, err
	}

	location, err := normalizeCondition("location", in.Location)
	if err != nil {
		return nil, err
	}

	if device == nil && location == nil {
		return nil, apierrors.Unprocessable("rule needs at least one condition (device_type or location)")
	}

	return &models.Rule{
		TargetName: name,
		DeviceType: device,
		Location:   location,
		URL:        dest.String(),
	}, nil
}

// normalizeCondition validates an optional condition: operator is/is_not and
// at least one non-empty value. Values are trimmed; empties are dropped.
func normalizeCondition(field string, c *models.RuleCondition) (*models.RuleCondition, error) {
	if c == nil {
		return nil, nil
	}

	if c.Type != models.ConditionIs && c.Type != models.ConditionIsNot {
		return nil, apierrors.Unprocessable(field + " condition type must be \"is\" or \"is_not\"")
	}

	values := make([]string, 0, len(c.Values))

	for _, v := range c.Values {
		if v = strings.TrimSpace(v); v != "" {
			values = append(values, v)
		}
	}

	if len(values) == 0 {
		return nil, apierrors.Unprocessable(field + " condition needs at least one value")
	}

	return &models.RuleCondition{Type: c.Type, Values: values}, nil
}
