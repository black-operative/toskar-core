package profiles

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/yeixio/toskar-core/internal/tools"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Manager handles profile CRUD in SQLite.
type Manager struct {
	db *sql.DB
}

func NewManager(db *sql.DB) *Manager {
	return &Manager{db: db}
}

// EnsurePresets seeds built-in presets if missing.
// Also soft-migrates Programming from prefer_local → automatic so paired
// computers work without manual role pins.
func (m *Manager) EnsurePresets(ctx context.Context) error {
	for _, p := range BuiltInPresets() {
		var exists int
		_ = m.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM profiles WHERE id = ?`, p.ID).Scan(&exists)
		if exists == 0 {
			if _, err := m.Create(ctx, p); err != nil {
				return err
			}
			continue
		}
		if p.ID == PresetProgramming {
			existing, err := m.Get(ctx, p.ID)
			if err != nil {
				return err
			}
			if existing.NodePolicy.Mode == "prefer_local" {
				existing.NodePolicy.Mode = "automatic"
				if err := m.Update(ctx, existing); err != nil {
					return err
				}
			}
			continue
		}
		if p.ID == PresetCustom {
			existing, err := m.Get(ctx, p.ID)
			if err != nil {
				return err
			}
			if len(existing.Roles) == 0 {
				existing.Roles = []contracts.ModelRole{{Role: "assistant", Required: false}}
				if err := m.Update(ctx, existing); err != nil {
					return err
				}
			}
		}
	}
	if err := m.migrateTeamProfiles(ctx); err != nil {
		return err
	}
	return m.mergeBuiltinTools(ctx)
}

// migrateTeamProfiles rewrites profiles saved for the old Team orchestrator,
// and roles with older names, onto the main pipeline (§37). Normalize does
// the mapping; this stores the result once.
func (m *Manager) migrateTeamProfiles(ctx context.Context) error {
	list, err := m.List(ctx)
	if err != nil {
		return err
	}
	for _, p := range list {
		if !needsNormalize(p) {
			continue
		}
		if err := m.Update(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

func needsNormalize(p Profile) bool {
	if p.OrchestratorID == legacyOrchestrator {
		return true
	}
	for _, r := range p.Roles {
		if _, ok := legacyRoles[r.Role]; ok {
			return true
		}
	}
	return false
}

// mergeBuiltinTools adds newly shipped tools without changing policies the user already saved.
func (m *Manager) mergeBuiltinTools(ctx context.Context) error {
	byID := map[string]Profile{}
	for _, preset := range BuiltInPresets() {
		byID[preset.ID] = preset
	}
	list, err := m.List(ctx)
	if err != nil {
		return err
	}
	for _, existing := range list {
		preset, ok := byID[existing.ID]
		if !ok {
			continue
		}
		merged := tools.MergeMissingTools(existing.Tools, preset.Tools)
		switch existing.ID {
		case PresetGeneral:
			merged = UpgradeGeneralTools(existing.Tools)
		case PresetProgramming:
			merged = upgradeIfUntouched(existing.Tools, preset.Tools, programmingStockSnapshots()...)
		case PresetResearch:
			merged = upgradeIfUntouched(existing.Tools, preset.Tools, researchStockSnapshots()...)
		}
		if samePolicies(merged, existing.Tools) {
			continue
		}
		existing.Tools = merged
		if err := m.Update(ctx, existing); err != nil {
			return err
		}
	}
	return nil
}

// List returns all profiles.
// Roles are loaded after the profiles cursor is closed so we never nest queries
// on the shared SQLite pool (MaxOpenConns=1), which would deadlock.
func (m *Manager) List(ctx context.Context) ([]Profile, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT id, name, purpose, orchestrator_id, node_policy_json, tools_json, COALESCE(knowledge_json, ''), COALESCE(orchestration_json, '')
		FROM profiles ORDER BY name`)
	if err != nil {
		return nil, err
	}
	var out []Profile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, p)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}

	for i := range out {
		roles, err := m.loadRoles(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Roles = roles
	}
	if out == nil {
		out = []Profile{}
	}
	return out, nil
}

// Get returns a profile by ID.
func (m *Manager) Get(ctx context.Context, id string) (Profile, error) {
	row := m.db.QueryRowContext(ctx, `
		SELECT id, name, purpose, orchestrator_id, node_policy_json, tools_json, COALESCE(knowledge_json, ''), COALESCE(orchestration_json, '')
		FROM profiles WHERE id = ?`, id)
	p, err := scanProfile(row)
	if err == sql.ErrNoRows {
		return Profile{}, fmt.Errorf("profile %q not found", id)
	}
	if err != nil {
		return Profile{}, err
	}
	p.Roles, err = m.loadRoles(ctx, id)
	return p, err
}

// Create inserts a new profile and returns it with a generated ID when needed.
func (m *Manager) Create(ctx context.Context, p Profile) (Profile, error) {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	p = Normalize(p)
	if err := Validate(p); err != nil {
		return Profile{}, err
	}
	nodePolicy, _ := json.Marshal(p.NodePolicy)
	tools, _ := json.Marshal(p.Tools)
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return Profile{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO profiles (id, name, purpose, orchestrator_id, node_policy_json, tools_json, knowledge_json, orchestration_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Purpose, p.OrchestratorID, string(nodePolicy), string(tools), knowledgeJSON(p.KnowledgeSources), orchestrationJSON(p.Orchestration)); err != nil {
		return Profile{}, err
	}
	if err := m.saveRolesTx(ctx, tx, p.ID, p.Roles); err != nil {
		return Profile{}, err
	}
	if err := tx.Commit(); err != nil {
		return Profile{}, err
	}
	return m.Get(ctx, p.ID)
}

// Update replaces an existing profile.
func (m *Manager) Update(ctx context.Context, p Profile) error {
	p = Normalize(p)
	if err := Validate(p); err != nil {
		return err
	}
	nodePolicy, _ := json.Marshal(p.NodePolicy)
	tools, _ := json.Marshal(p.Tools)
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `
		UPDATE profiles SET name=?, purpose=?, orchestrator_id=?, node_policy_json=?, tools_json=?, knowledge_json=?, orchestration_json=?, updated_at=datetime('now')
		WHERE id=?`,
		p.Name, p.Purpose, p.OrchestratorID, string(nodePolicy), string(tools), knowledgeJSON(p.KnowledgeSources), orchestrationJSON(p.Orchestration), p.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("profile %q not found", p.ID)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM profile_roles WHERE profile_id = ?`, p.ID); err != nil {
		return err
	}
	if err := m.saveRolesTx(ctx, tx, p.ID, p.Roles); err != nil {
		return err
	}
	return tx.Commit()
}

// Delete removes a profile. Built-in presets cannot be deleted (duplicate instead).
func (m *Manager) Delete(ctx context.Context, id string) error {
	if IsPreset(id) {
		return fmt.Errorf("built-in profile %q cannot be deleted; duplicate it to customize", id)
	}
	res, err := m.db.ExecContext(ctx, `DELETE FROM profiles WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("profile %q not found", id)
	}
	return nil
}

// ResetPreset puts one built-in profile back to how Yggdrasil ships it
// (spec §24). Other profiles cannot be reset; duplicate a built-in instead.
func (m *Manager) ResetPreset(ctx context.Context, id string) (Profile, error) {
	for _, preset := range BuiltInPresets() {
		if preset.ID != id {
			continue
		}
		if _, err := m.Get(ctx, id); err != nil {
			return m.Create(ctx, preset)
		}
		if err := m.Update(ctx, preset); err != nil {
			return Profile{}, err
		}
		return m.Get(ctx, id)
	}
	return Profile{}, fmt.Errorf("only built-in profiles can be reset to defaults")
}

// ResetToDefaults removes user-created profiles and restores built-in presets.
func (m *Manager) ResetToDefaults(ctx context.Context) error {
	items, err := m.List(ctx)
	if err != nil {
		return err
	}
	for _, p := range items {
		if IsPreset(p.ID) {
			continue
		}
		if err := m.Delete(ctx, p.ID); err != nil {
			return err
		}
	}
	for _, preset := range BuiltInPresets() {
		if _, err := m.Get(ctx, preset.ID); err != nil {
			if _, err := m.Create(ctx, preset); err != nil {
				return err
			}
			continue
		}
		if err := m.Update(ctx, preset); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) loadRoles(ctx context.Context, profileID string) ([]contracts.ModelRole, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT role, COALESCE(model_id,''), COALESCE(node_id,''), required
		FROM profile_roles WHERE profile_id = ?`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	roles := []contracts.ModelRole{}
	for rows.Next() {
		var r contracts.ModelRole
		var req int
		if err := rows.Scan(&r.Role, &r.ModelID, &r.NodeID, &req); err != nil {
			return nil, err
		}
		r.Required = req != 0
		roles = append(roles, r)
	}
	return roles, rows.Err()
}

func (m *Manager) saveRolesTx(ctx context.Context, tx *sql.Tx, profileID string, roles []contracts.ModelRole) error {
	for _, r := range roles {
		req := 0
		if r.Required {
			req = 1
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO profile_roles (id, profile_id, role, model_id, node_id, required)
			VALUES (?, ?, ?, ?, ?, ?)`,
			uuid.NewString(), profileID, r.Role, nullStr(r.ModelID), nullStr(r.NodeID), req)
		if err != nil {
			return err
		}
	}
	return nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanProfile(row scannable) (Profile, error) {
	var p Profile
	var nodePolicyJSON, toolsJSON, knowledge, orchestration string
	if err := row.Scan(&p.ID, &p.Name, &p.Purpose, &p.OrchestratorID, &nodePolicyJSON, &toolsJSON, &knowledge, &orchestration); err != nil {
		return Profile{}, err
	}
	decodeProfileMeta(&p, nodePolicyJSON, toolsJSON)
	if knowledge != "" {
		_ = json.Unmarshal([]byte(knowledge), &p.KnowledgeSources)
	}
	if orchestration != "" {
		_ = json.Unmarshal([]byte(orchestration), &p.Orchestration)
	}
	return p, nil
}

// orchestrationJSON stores a profile's advanced controls, or NULL when it
// keeps every default.
func orchestrationJSON(o contracts.OrchestrationPolicy) any {
	b, _ := json.Marshal(o)
	if string(b) == "{}" {
		return nil
	}
	return string(b)
}

// knowledgeJSON stores connected knowledge source ids, or NULL when there are none.
func knowledgeJSON(ids []string) any {
	if len(ids) == 0 {
		return nil
	}
	b, _ := json.Marshal(ids)
	return string(b)
}

func decodeProfileMeta(p *Profile, nodePolicyJSON, toolsJSON string) {
	_ = json.Unmarshal([]byte(nodePolicyJSON), &p.NodePolicy)
	_ = json.Unmarshal([]byte(toolsJSON), &p.Tools)
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
