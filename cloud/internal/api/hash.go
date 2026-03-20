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
// serialising all active gateways + services + sensors into a canonical JSON
// blob, then storing the result in config_state. Call this after every
// mutation that would change what Conf-Agent deploys.
func recomputeConfigHash(ctx context.Context, pool *pgxpool.Pool, qubeID string) (string, error) {
	type sensorRow struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		TemplateID    string `json:"template_id"`
		AddressParams any    `json:"address_params"`
		TagsJSON      any    `json:"tags_json"`
	}
	type gwRow struct {
		ID           string      `json:"id"`
		Name         string      `json:"name"`
		Protocol     string      `json:"protocol"`
		Host         string      `json:"host"`
		Port         int         `json:"port"`
		ConfigJSON   any         `json:"config_json"`
		ServiceImage string      `json:"service_image"`
		Sensors      []sensorRow `json:"sensors"`
	}

	rows, err := pool.Query(ctx,
		`SELECT g.id, g.name, g.protocol, g.host, g.port, g.config_json, g.service_image
		 FROM gateways g
		 WHERE g.qube_id=$1 AND g.status='active'
		 ORDER BY g.created_at ASC`, qubeID)
	if err != nil {
		return "", fmt.Errorf("query gateways: %w", err)
	}
	defer rows.Close()

	var gateways []gwRow
	for rows.Next() {
		var gw gwRow
		var cfgRaw, imgRaw []byte
		if err := rows.Scan(&gw.ID, &gw.Name, &gw.Protocol,
			&gw.Host, &gw.Port, &cfgRaw, &imgRaw); err != nil {
			continue
		}
		json.Unmarshal(cfgRaw, &gw.ConfigJSON)
		gw.ServiceImage = string(imgRaw)

		// Fetch sensors for this gateway
		srows, _ := pool.Query(ctx,
			`SELECT id, name, template_id, address_params, tags_json
			 FROM sensors WHERE gateway_id=$1 AND status='active' ORDER BY created_at ASC`,
			gw.ID)
		for srows.Next() {
			var s sensorRow
			var apRaw, tagsRaw []byte
			if err := srows.Scan(&s.ID, &s.Name, &s.TemplateID, &apRaw, &tagsRaw); err == nil {
				json.Unmarshal(apRaw, &s.AddressParams)
				json.Unmarshal(tagsRaw, &s.TagsJSON)
				gw.Sensors = append(gw.Sensors, s)
			}
		}
		srows.Close()
		gateways = append(gateways, gw)
	}

	canonical, err := json.Marshal(map[string]any{
		"qube_id":  qubeID,
		"gateways": gateways,
	})
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	sum := sha256.Sum256(canonical)
	hash := hex.EncodeToString(sum[:])

	_, err = pool.Exec(ctx,
		`UPDATE config_state SET hash=$1, generated_at=NOW(), config_snapshot=$2 WHERE qube_id=$3`,
		hash, canonical, qubeID)
	if err != nil {
		return "", fmt.Errorf("update config_state: %w", err)
	}
	return hash, nil
}
