package sqlite

import (
	agentdata "certainstats/internal/agent_data"
	"certainstats/internal/base"
	baseresponse "certainstats/internal/base/response"
	"certainstats/internal/dashboard/accessrules"
	"certainstats/internal/store"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (s *Store) DashboardCreate(ctx context.Context, d store.Dashboard) error {
	rules, err := json.Marshal(d.AccessRules)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO dashboards (dashboard_id, user_id, slug, title, access_rules)
		 VALUES (?, ?, ?, ?, ?)`,
		d.DashboardID, d.UserID, d.Slug, d.Title, string(rules),
	)
	return err
}

func (s *Store) DashboardList(ctx context.Context, userID string) ([]store.Dashboard, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT dashboard_id, user_id, slug, title, access_rules
		 FROM dashboards WHERE user_id = ? ORDER BY title`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []store.Dashboard
	for rows.Next() {
		var d store.Dashboard
		var rulesJSON string
		if err := rows.Scan(&d.DashboardID, &d.UserID, &d.Slug, &d.Title, &rulesJSON); err != nil {
			return nil, err
		}
		rules, err := accessrules.ParseRules(rulesJSON)
		if err != nil {
			return nil, err
		}
		d.AccessRules = rules
		out = append(out, d)
	}
	if out == nil {
		out = []store.Dashboard{}
	}
	return out, rows.Err()
}

func (s *Store) DashboardGetInfo(ctx context.Context, dashboard_id string, userID string) (store.Dashboard, error) {
	var rulesJSON string
	var d store.Dashboard

	err := s.db.QueryRowContext(ctx,
		`SELECT dashboard_id, user_id, slug, title, access_rules
		 FROM dashboards WHERE user_id = ? and dashboard_id = ?`,
		userID, dashboard_id,
	).Scan(&d.DashboardID, &d.UserID, &d.Slug, &d.Title, &rulesJSON)
	if err == sql.ErrNoRows {
		return store.Dashboard{}, sql.ErrNoRows
	}
	if err != nil {
		return store.Dashboard{}, err
	}

	rules, err := accessrules.ParseRules(rulesJSON)
	if err != nil {
		return store.Dashboard{}, err
	}
	d.AccessRules = rules

	return d, nil
}
func (s *Store) DashboardAddAgents(ctx context.Context, d store.Dashboard, a []baseresponse.CreateDashboardReqAgent) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	for _, agent := range a {
		publicID := fmt.Sprintf("pub_%s", agentdata.GenerateRandomString(36))

		_, err = tx.Exec(`
			INSERT INTO dashboard_agents (dashboard_id, agent_id, agent_public_id, agent_public_nickname) 
			VALUES (?, ?, ?, ?)
		`, d.DashboardID, agent.AgentID, publicID, agent.Alias)

		if err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (s *Store) DashboardDelete(ctx context.Context, dashboard_id string, userID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		DELETE FROM dashboards WHERE dashboard_id = ? and user_id = ?
	`, dashboard_id, userID)

	if err != nil {
		tx.Rollback()
		return err
	}
	if err == sql.ErrNoRows {
		tx.Rollback()
		return sql.ErrNoRows
	}
	_, err = tx.Exec(`
		DELETE FROM dashboard_agents WHERE dashboard_id = ?
	`, dashboard_id)

	if err != nil {
		tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (s *Store) DashboardGetBySlug(ctx context.Context, slug string) (*store.Dashboard, error) {
	var d store.Dashboard
	var rulesJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT dashboard_id, user_id, slug, title, access_rules
		 FROM dashboards WHERE slug = ?`, slug,
	).Scan(&d.DashboardID, &d.UserID, &d.Slug, &d.Title, &rulesJSON)
	if err != nil {
		return nil, err
	}
	rules, err := accessrules.ParseRules(rulesJSON)
	if err != nil {
		return nil, err
	}
	d.AccessRules = rules
	return &d, nil
}

// safeColumnMap maps every FeaturesList name to its qualified SQL column.
// This is the single place that name→column translation lives.
// Never built from request input — only from the validated rule's field names.
var safeColumnMap = map[string]string{
	"is_online":     "a.is_online",
	"uptime":        "a.uptime",
	"linux_version": "a.linux_version",
	"cpu_model":     "a.cpu_model",
	"cpu_cores":     "a.cpu_cores",
	"ram_size":      "a.ram_size",
	"swap_size":     "a.swap_size",
	"disk_size":     "a.disk_size",
}

func (s *Store) DashboardGetPublicAgents(
	ctx context.Context,
	slug string,
	rule accessrules.AccessRule,
) ([]store.PublicAgent, error) {
	allowedFields := rule.FeatureSet()

	type selectedField struct {
		alias  string
		column string
	}
	var selected []selectedField

	// 1. Pre-Query: Build a SELECT list dynamically based on allowed rules.
	for field := range allowedFields {
		if col, ok := safeColumnMap[field]; ok {
			selected = append(selected, selectedField{alias: field, column: col})
		}
	}

	colExpr := []string{
		"da.agent_public_id",
		"COALESCE(NULLIF(da.agent_public_nickname,''), NULLIF(a.nickname,''), a.agent_id) AS display_name",
		"a.agent_id",
		"a.total_rx_bytes",
		"a.total_tx_bytes",
	}
	for _, f := range selected {
		colExpr = append(colExpr, fmt.Sprintf("%s AS %s", f.column, f.alias))
	}

	query := fmt.Sprintf(`
		SELECT %s 
		FROM agents a 
		JOIN dashboard_agents da ON a.agent_id = da.agent_id 
		JOIN dashboards d ON da.dashboard_id = d.dashboard_id 
		WHERE d.slug = ?`,
		strings.Join(colExpr, ", "),
	)

	// Check if disk metrics are allowed under the public rule before querying DB
	metricsSet := rule.MetricSet()
	_, allowDiskQuery := metricsSet["agent_disk_read_bytes"]
	if !allowDiskQuery {
		_, allowDiskQuery = metricsSet["agent_disk_write_bytes"]
	}

	disksMap := make(map[string][]baseresponse.DiskOdometer)
	if allowDiskQuery {
		diskRows, err := s.db.QueryContext(ctx, `
			SELECT ado.agent_id, ado.path, ado.read_bytes, ado.write_bytes
			FROM   agent_disk_odometers ado
			JOIN   dashboard_agents da ON ado.agent_id = da.agent_id
			JOIN   dashboards d ON da.dashboard_id = d.dashboard_id
			WHERE  d.slug = ?`,
			slug,
		)
		if err == nil {
			for diskRows.Next() {
				var agentID string
				var path string
				var rVal, wVal uint64
				if err := diskRows.Scan(&agentID, &path, &rVal, &wVal); err == nil {
					disksMap[agentID] = append(disksMap[agentID], baseresponse.DiskOdometer{
						Path:       path,
						ReadBytes:  &rVal,
						WriteBytes: &wVal,
					})
				}
			}
			diskRows.Close() // Release connection back to pool explicitly
		}
	}

	rows, err := s.db.QueryContext(ctx, query, slug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []store.PublicAgent
	for rows.Next() {
		var pa store.PublicAgent
		var realAgentID string
		var totalRx, totalTx uint64

		// Prepare dynamic destination slice for scanning
		dest := []any{&pa.PublicID, &pa.Name, &realAgentID, &totalRx, &totalTx}
		tmp := make(map[string]*any, len(selected))
		for _, f := range selected {
			var v any
			tmp[f.alias] = &v
			dest = append(dest, &v)
		}

		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}

		// 2. Safe Coercion & Post-Query Enforcement
		// We extract the value, check cross-DB types, and assign to pointers ONLY
		// if the rule explicitly permits it. If not, it remains nil.

		if v := tmp["is_online"]; v != nil && *v != nil {
			var bVal bool
			if b, ok := (*v).(bool); ok { // Postgres
				bVal = b
			} else if i, ok := (*v).(int64); ok { // SQLite
				bVal = i != 0
			}
			pa.IsOnline = &bVal
		}

		if v := tmp["uptime"]; v != nil && *v != nil {
			if i, ok := (*v).(int64); ok {
				val := uint32(i)
				pa.Uptime = &val
			}
		}

		if v := tmp["linux_version"]; v != nil && *v != nil {
			if s, ok := (*v).(string); ok {
				pa.LinuxVersion = &s
			} else if b, ok := (*v).([]byte); ok { // Fallback for some DB drivers
				str := string(b)
				pa.LinuxVersion = &str
			}
		}

		if v := tmp["cpu_model"]; v != nil && *v != nil {
			if s, ok := (*v).(string); ok {
				pa.CpuModel = &s
			} else if b, ok := (*v).([]byte); ok {
				str := string(b)
				pa.CpuModel = &str
			}
		}

		if v := tmp["cpu_cores"]; v != nil && *v != nil {
			if i, ok := (*v).(int64); ok {
				val := uint16(i)
				pa.CpuCores = &val
			}
		}

		if v := tmp["ram_size"]; v != nil && *v != nil {
			if i, ok := (*v).(int64); ok {
				val := uint64(i)
				pa.RamSize = &val
			}
		}

		if v := tmp["swap_size"]; v != nil && *v != nil {
			if i, ok := (*v).(int64); ok {
				val := uint64(i)
				pa.SwapSize = &val
			}
		}

		if v := tmp["disk_size"]; v != nil && *v != nil {
			if i, ok := (*v).(int64); ok {
				val := uint64(i)
				pa.DiskSize = &val
			}
		}

		// Apply public access rules for total traffic metrics granularly
		metricsSet := rule.MetricSet()

		_, allowRx := metricsSet["agent_rx_bytes"]
		_, allowTx := metricsSet["agent_tx_bytes"]

		if allowRx || allowTx {
			pa.Net = &baseresponse.NetOdometer{}
			if allowRx {
				val := totalRx
				pa.Net.TotalRxBytes = &val
			}
			if allowTx {
				val := totalTx
				pa.Net.TotalTxBytes = &val
			}
		}

		_, allowRead := metricsSet["agent_disk_read_bytes"]
		_, allowWrite := metricsSet["agent_disk_write_bytes"]

		if allowRead || allowWrite {
			rawDisks := disksMap[realAgentID]
			if len(rawDisks) > 0 {
				pa.Disks = make([]baseresponse.DiskOdometer, len(rawDisks))
				for idx, d := range rawDisks {
					pa.Disks[idx] = baseresponse.DiskOdometer{
						Path: d.Path,
					}
					if allowRead && d.ReadBytes != nil {
						val := *d.ReadBytes
						pa.Disks[idx].ReadBytes = &val
					}
					if allowWrite && d.WriteBytes != nil {
						val := *d.WriteBytes
						pa.Disks[idx].WriteBytes = &val
					}
				}
			}
		}

		out = append(out, pa)
	}

	if out == nil {
		out = []store.PublicAgent{}
	}
	return out, rows.Err()
}

func (s *Store) DashboardFindAgentbyPublicID(ctx context.Context, dashboardID string, publicAgentID string) (base.FindAgentByPublicID, error) {
	var rulesJSON, ownerID, realAgentID string
	err := s.db.QueryRow(`
		SELECT d.access_rules, d.user_id, da.agent_id 
		FROM dashboards d
		JOIN dashboard_agents da ON d.dashboard_id = da.dashboard_id
		WHERE d.dashboard_id = ? AND da.agent_public_id = ?
	`, dashboardID, publicAgentID).Scan(&rulesJSON, &ownerID, &realAgentID)
	if err != nil {
		return base.FindAgentByPublicID{}, err
	}

	return base.FindAgentByPublicID{
		RulesJSON:   rulesJSON,
		OwnerID:     ownerID,
		RealAgentID: realAgentID,
	}, nil
}

func (s *Store) DashboardGetAgents(ctx context.Context, dashboardID string, userID string) ([]store.PublicAgentIdentity, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT da.agent_id, da.agent_public_id, da.agent_public_nickname
		FROM dashboards d
		JOIN dashboard_agents da ON d.dashboard_id = da.dashboard_id
		WHERE d.dashboard_id = ? AND d.user_id = ?`, dashboardID, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []store.PublicAgentIdentity

	for rows.Next() {
		var id store.PublicAgentIdentity
		if err := rows.Scan(&id.AgentID, &id.PublicAgentID, &id.PublicAgentNickname); err != nil {
			return nil, err
		}
		agents = append(agents, id)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return agents, nil
}

// DashboardUpdate fully updates the dashboard and smartly syncs agents using DashboardID
func (s *Store) DashboardUpdate(ctx context.Context, d store.Dashboard, newAgents []baseresponse.CreateDashboardReqAgent) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. Prepare ACLs
	rules, err := json.Marshal(d.AccessRules)
	if err != nil {
		return err
	}

	// 2. Update the main dashboard record using the immutable dashboard_id
	res, err := tx.ExecContext(ctx,
		`UPDATE dashboards 
         SET title = ?, slug = ?, access_rules = ? 
         WHERE dashboard_id = ? AND user_id = ?`,
		d.Title, d.Slug, string(rules), d.DashboardID, d.UserID,
	)
	if err != nil {
		return err // Likely a UNIQUE constraint error if the new slug is taken
	}

	// Ensure the dashboard actually existed and belonged to the user
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return fmt.Errorf("dashboard not found or unauthorized")
	}

	// 3. Fetch existing agents to compare in-memory
	rows, err := tx.QueryContext(ctx, `
        SELECT agent_id, agent_public_nickname 
        FROM dashboard_agents 
        WHERE dashboard_id = ?`,
		d.DashboardID,
	)
	if err != nil {
		return err
	}

	existingAgents := make(map[string]string)
	for rows.Next() {
		var agentID, nickname string
		if err := rows.Scan(&agentID, &nickname); err != nil {
			rows.Close()
			return err
		}
		existingAgents[agentID] = nickname
	}
	rows.Close()

	// 4. Diffing Logic: Figure out what actually changed
	var toAdd []baseresponse.CreateDashboardReqAgent
	var toUpdate []baseresponse.CreateDashboardReqAgent
	var toDelete []string

	newAgentsMap := make(map[string]string)
	for _, na := range newAgents {
		newAgentsMap[na.AgentID] = na.Alias
		existingNickname, exists := existingAgents[na.AgentID]

		if !exists {
			toAdd = append(toAdd, na)
		} else if existingNickname != na.Alias {
			toUpdate = append(toUpdate, na)
		}
	}

	for existingID := range existingAgents {
		if _, exists := newAgentsMap[existingID]; !exists {
			toDelete = append(toDelete, existingID)
		}
	}

	// 5. Fire SQL ONLY for the exact agent changes detected
	if len(toDelete) > 0 {
		stmt, err := tx.PrepareContext(ctx, `DELETE FROM dashboard_agents WHERE dashboard_id = ? AND agent_id = ?`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, id := range toDelete {
			if _, err := stmt.ExecContext(ctx, d.DashboardID, id); err != nil {
				return err
			}
		}
	}

	if len(toAdd) > 0 {
		stmt, err := tx.PrepareContext(ctx, `
            INSERT INTO dashboard_agents (dashboard_id, agent_id, agent_public_id, agent_public_nickname) 
            VALUES (?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, agent := range toAdd {
			publicID := fmt.Sprintf("pub_%d_%s", time.Now().UnixMicro(), agentdata.GenerateRandomString(8))
			if _, err := stmt.ExecContext(ctx, d.DashboardID, agent.AgentID, publicID, agent.Alias); err != nil {
				return err
			}
		}
	}

	if len(toUpdate) > 0 {
		stmt, err := tx.PrepareContext(ctx, `
            UPDATE dashboard_agents 
            SET agent_public_nickname = ? 
            WHERE dashboard_id = ? AND agent_id = ?`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, agent := range toUpdate {
			if _, err := stmt.ExecContext(ctx, agent.Alias, d.DashboardID, agent.AgentID); err != nil {
				return err
			}
		}
	}

	// 6. Commit the transaction!
	return tx.Commit()
}

func (s *Store) DashboardGetPulseConfig(ctx context.Context, dashboardID string) (*store.Dashboard, []store.PublicAgentIdentity, error) {
	// 1. Get Dashboard (rules)
	var rulesJSON string
	var d store.Dashboard
	err := s.db.QueryRowContext(ctx,
		`SELECT dashboard_id, user_id, slug, title, access_rules
         FROM dashboards WHERE dashboard_id = ?`,
		dashboardID,
	).Scan(&d.DashboardID, &d.UserID, &d.Slug, &d.Title, &rulesJSON)
	if err != nil {
		return nil, nil, err
	}
	rules, err := accessrules.ParseRules(rulesJSON)
	if err != nil {
		return nil, nil, err
	}
	d.AccessRules = rules

	// 2. Get Agents
	rows, err := s.db.QueryContext(ctx, `
        SELECT agent_id, agent_public_id, agent_public_nickname
        FROM dashboard_agents
        WHERE dashboard_id = ?`, dashboardID,
	)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var agents []store.PublicAgentIdentity
	for rows.Next() {
		var id store.PublicAgentIdentity
		if err := rows.Scan(&id.AgentID, &id.PublicAgentID, &id.PublicAgentNickname); err != nil {
			return nil, nil, err
		}
		agents = append(agents, id)
	}

	return &d, agents, nil
}
