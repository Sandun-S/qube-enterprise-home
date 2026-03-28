package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// recomputeConfigHash rebuilds the SHA-256 config hash for a Qube by
// serialising all active readers + sensors into canonical JSON,
// then storing the result in config_state. Call after every mutation
// that changes what conf-agent deploys (or writes to SQLite).
func recomputeConfigHash(ctx context.Context, pool *pgxpool.Pool, qubeID string) (string, error) {
	type sensorRow struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Config    any    `json:"config_json"`
		Tags      any    `json:"tags_json"`
		Output    string `json:"output"`
		TableName string `json:"table_name"`
	}
	type readerRow struct {
		ID       string      `json:"id"`
		Name     string      `json:"name"`
		Protocol string      `json:"protocol"`
		Config   any         `json:"config_json"`
		Sensors  []sensorRow `json:"sensors"`
	}

	rows, err := pool.Query(ctx,
		`SELECT rd.id, rd.name, rd.protocol, rd.config_json
		 FROM readers rd
		 WHERE rd.qube_id=$1 AND rd.status='active'
		 ORDER BY rd.created_at ASC`, qubeID)
	if err != nil {
		return "", fmt.Errorf("query readers: %w", err)
	}
	defer rows.Close()

	var readers []readerRow
	for rows.Next() {
		var rd readerRow
		var cfgRaw []byte
		if err := rows.Scan(&rd.ID, &rd.Name, &rd.Protocol, &cfgRaw); err != nil {
			continue
		}
		json.Unmarshal(cfgRaw, &rd.Config)

		// Fetch sensors for this reader
		srows, _ := pool.Query(ctx,
			`SELECT id, name, config_json, tags_json, output, table_name
			 FROM sensors WHERE reader_id=$1 AND status='active'
			 ORDER BY created_at ASC`, rd.ID)
		for srows.Next() {
			var s sensorRow
			var sCfgRaw, sTagsRaw []byte
			if err := srows.Scan(&s.ID, &s.Name, &sCfgRaw, &sTagsRaw, &s.Output, &s.TableName); err == nil {
				json.Unmarshal(sCfgRaw, &s.Config)
				json.Unmarshal(sTagsRaw, &s.Tags)
				rd.Sensors = append(rd.Sensors, s)
			}
		}
		srows.Close()
		readers = append(readers, rd)
	}

	canonical, err := json.Marshal(map[string]any{
		"qube_id": qubeID,
		"readers": readers,
	})
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	sum := sha256.Sum256(canonical)
	hash := hex.EncodeToString(sum[:])

	// Update config_state with new hash + version bump
	_, err = pool.Exec(ctx,
		`UPDATE config_state
		 SET hash=$1, config_version=config_version+1, generated_at=NOW(), config_snapshot=$2
		 WHERE qube_id=$3`,
		hash, canonical, qubeID)
	if err != nil {
		return "", fmt.Errorf("update config_state: %w", err)
	}

	// Push config change notification via WebSocket (if connected)
	NotifyConfigChange(pool, globalHub, qubeID, hash)

	return hash, nil
}
