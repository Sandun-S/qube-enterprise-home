// run.go — Cloud polling loop (v1 legacy, superseded in v2)
//
// In v1 (conf-agent-master), Run() polled the conf-api portal via
// HMAC-signed HTTP POST requests to receive commands and send back results.
//
// In v2 (enterprise), this entire flow has been replaced by the enterprise
// agent (agent/agent.go) which:
//   - Connects to the cloud API via WebSocket (real-time config push + commands)
//   - Falls back to TP-API HTTP polling when WebSocket is disconnected
//   - Writes received config to SQLite (shared with reader containers)
//   - Deploys Docker containers via docker stack deploy / docker compose up
//
// The v1 HMAC signing scheme (#mit_Agent~client / #mit_Portal~server) has been
// replaced by the enterprise TP-API HMAC (HMAC-SHA256 of qubeID+":"+orgSecret).
//
// This file is retained for historical reference only.
package http
