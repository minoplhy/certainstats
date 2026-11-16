package crowdsec

// queries holds every statement the health reader runs, written out in full for
// each database dialect. Nothing is assembled at run time: values, limits
// included, are always placeholders. "type" and "until" are keywords in some
// dialects and are quoted the way each one wants.
type queries struct {
	machinesOS string // machines with OS columns (newer CrowdSec)
	machines   string // machines without them
	bouncers   string
	active     string // active bans by origin; args: now, limit
	metrics    string // newest bouncer usage metrics first; args: limit
}

var sqliteQueries = queries{
	machinesOS: `SELECT machine_id, last_heartbeat, version, ip_address, is_validated, osname, osversion FROM machines ORDER BY machine_id LIMIT ?`,
	machines:   `SELECT machine_id, last_heartbeat, version, ip_address, is_validated FROM machines ORDER BY machine_id LIMIT ?`,
	bouncers:   `SELECT name, last_pull, "type", version, ip_address, revoked FROM bouncers ORDER BY name LIMIT ?`,
	active: `SELECT origin, CASE WHEN origin = 'lists' THEN scenario ELSE '' END AS list, COUNT(DISTINCT value) FROM decisions
		WHERE "until" > ? AND "type" = 'ban' AND NOT simulated AND scope IN ('Ip', 'Range') GROUP BY 1, 2 LIMIT ?`,
	metrics: `SELECT generated_by, received_at, payload FROM metrics WHERE generated_type = 'RC' ORDER BY id DESC LIMIT ?`,
}

var postgresQueries = queries{
	machinesOS: `SELECT machine_id, last_heartbeat, version, ip_address, is_validated, osname, osversion FROM machines ORDER BY machine_id LIMIT $1`,
	machines:   `SELECT machine_id, last_heartbeat, version, ip_address, is_validated FROM machines ORDER BY machine_id LIMIT $1`,
	bouncers:   `SELECT name, last_pull, "type", version, ip_address, revoked FROM bouncers ORDER BY name LIMIT $1`,
	active: `SELECT origin, CASE WHEN origin = 'lists' THEN scenario ELSE '' END AS list, COUNT(DISTINCT value) FROM decisions
		WHERE "until" > $1 AND "type" = 'ban' AND NOT simulated AND scope IN ('Ip', 'Range') GROUP BY 1, 2 LIMIT $2`,
	metrics: `SELECT generated_by, received_at, payload FROM metrics WHERE generated_type = 'RC' ORDER BY id DESC LIMIT $1`,
}

var mysqlQueries = queries{
	machinesOS: "SELECT machine_id, last_heartbeat, version, ip_address, is_validated, osname, osversion FROM machines ORDER BY machine_id LIMIT ?",
	machines:   "SELECT machine_id, last_heartbeat, version, ip_address, is_validated FROM machines ORDER BY machine_id LIMIT ?",
	bouncers:   "SELECT name, last_pull, `type`, version, ip_address, revoked FROM bouncers ORDER BY name LIMIT ?",
	active: "SELECT origin, CASE WHEN origin = 'lists' THEN scenario ELSE '' END AS list, COUNT(DISTINCT value) FROM decisions " +
		"WHERE `until` > ? AND `type` = 'ban' AND NOT simulated AND scope IN ('Ip', 'Range') GROUP BY 1, 2 LIMIT ?",
	metrics: "SELECT generated_by, received_at, payload FROM metrics WHERE generated_type = 'RC' ORDER BY id DESC LIMIT ?",
}

// canWritePostgres asks whether the session's user may write to bouncers. Asking writes nothing.
const canWritePostgres = `SELECT has_table_privilege(current_user, 'bouncers', 'INSERT')`
