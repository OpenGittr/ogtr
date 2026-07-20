package stores

import (
	"database/sql"
	"encoding/json"
	"errors"

	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/models"
)

// RuleStore reads and writes the link_rules table. Every query is org-scoped
// (FEATURES.md INV-6). The conditions + destination live in the rule JSON
// column; target_name is mirrored into its own column for readability.
type RuleStore struct{}

// NewRuleStore builds a RuleStore.
func NewRuleStore() *RuleStore { return &RuleStore{} }

// ruleBody is the persisted shape of the rule JSON column.
type ruleBody struct {
	TargetName string                `json:"target_name"`
	DeviceType *models.RuleCondition `json:"device_type,omitempty"`
	Location   *models.RuleCondition `json:"location,omitempty"`
	URL        string                `json:"url"`
}

const ruleColumns = "id, org_id, link_id, target_name, rule, created_at"

// Create inserts a rule and returns the stored row.
func (s *RuleStore) Create(ctx *gofr.Context, r *models.Rule) (*models.Rule, error) {
	body, err := marshalRule(r)
	if err != nil {
		return nil, err
	}

	res, err := ctx.SQL.ExecContext(ctx,
		"INSERT INTO link_rules (org_id, link_id, target_name, rule) VALUES (?, ?, ?, ?)",
		r.OrgID, r.LinkID, r.TargetName, body)
	if err != nil {
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}

	return s.GetByID(ctx, r.OrgID, id)
}

// GetByID fetches a rule within the org; (nil, nil) when absent.
func (*RuleStore) GetByID(ctx *gofr.Context, orgID, id int64) (*models.Rule, error) {
	row := ctx.SQL.QueryRowContext(ctx,
		"SELECT "+ruleColumns+" FROM link_rules WHERE id = ? AND org_id = ?", id, orgID)

	r, err := scanRuleRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return r, nil
}

// ListByLink returns a link's rules in creation order — the order they are
// evaluated in on resolution (first match wins, FEATURES.md §3.1).
func (*RuleStore) ListByLink(ctx *gofr.Context, orgID, linkID int64) ([]models.Rule, error) {
	rows, err := ctx.SQL.QueryContext(ctx,
		"SELECT "+ruleColumns+" FROM link_rules WHERE org_id = ? AND link_id = ? ORDER BY id",
		orgID, linkID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	rules := []models.Rule{}

	for rows.Next() {
		r, err := scanRuleRow(rows)
		if err != nil {
			return nil, err
		}

		rules = append(rules, *r)
	}

	return rules, rows.Err()
}

// Update replaces a rule's name, conditions and destination in place (its
// position in the evaluation order is kept).
func (s *RuleStore) Update(ctx *gofr.Context, r *models.Rule) (*models.Rule, error) {
	body, err := marshalRule(r)
	if err != nil {
		return nil, err
	}

	_, err = ctx.SQL.ExecContext(ctx,
		"UPDATE link_rules SET target_name = ?, rule = ? WHERE id = ? AND org_id = ?",
		r.TargetName, body, r.ID, r.OrgID)
	if err != nil {
		return nil, err
	}

	return s.GetByID(ctx, r.OrgID, r.ID)
}

// Delete removes a rule.
func (*RuleStore) Delete(ctx *gofr.Context, orgID, id int64) error {
	_, err := ctx.SQL.ExecContext(ctx,
		"DELETE FROM link_rules WHERE id = ? AND org_id = ?", id, orgID)

	return err
}

// marshalRule serializes the rule JSON column as a string, not []byte: the
// MySQL driver sends []byte with the binary charset, which JSON columns
// reject (error 3144).
func marshalRule(r *models.Rule) (string, error) {
	raw, err := json.Marshal(ruleBody{
		TargetName: r.TargetName,
		DeviceType: r.DeviceType,
		Location:   r.Location,
		URL:        r.URL,
	})

	return string(raw), err
}

func scanRuleRow(row rowScanner) (*models.Rule, error) {
	var (
		r   models.Rule
		raw []byte
	)

	if err := row.Scan(&r.ID, &r.OrgID, &r.LinkID, &r.TargetName, &raw, &r.CreatedAt); err != nil {
		return nil, err
	}

	var body ruleBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}

	r.DeviceType = body.DeviceType
	r.Location = body.Location
	r.URL = body.URL

	return &r, nil
}
