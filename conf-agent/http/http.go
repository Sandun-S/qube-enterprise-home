// Package http implements the local device management HTTP server.
//
// This is the v1 conf-agent-master local server preserved and updated for v2.
// It serves the local web UI on the configured Port (default 8081) for:
//   - Local device info page   GET /
//   - Device reboot            GET /reboot?mtnKey=<key>
//   - Device shutdown          GET /shutdown?mtnKey=<key>
//   - Network IP reset         GET /reset-ips?mtnKey=<key>
//   - Filesystem repair        GET /repair?mtnKey=<key>
//   - Log viewer               GET /logs?mtnKey=<key>&logType=<type>
//   - Data backup              GET /backup   (not yet implemented)
//   - Image backup             GET /create-image   (not yet implemented)
//   - Image restore            GET /restore-image  (not yet implemented)
//
// All maintenance actions are protected by the maintain key from /boot/mit.txt.
//
// In v2, cloud command dispatch (set_wifi, set_eth, get_info, etc.) is handled
// by the enterprise agent (agent/agent.go) via WebSocket or TP-API polling.
// This server is purely for local on-device management when the operator has
// physical/network access to the Qube.
package http

import (
	"fmt"
	"net/http"
	"net/http/pprof"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"conf-agent/configs"

	"github.com/sirupsen/logrus"
)

// ============================================================================

var deviceID string
var conf *configs.Config
var mit *configs.MitTxt
var log *logrus.Logger

// ============================================================================

// Init registers HTTP handlers and starts the local management server.
// Equivalent to v1 Init() but without the conf-api polling machinery.
func Init(l *logrus.Logger, cfg *configs.Config, device *configs.MitTxt) {
	conf = cfg
	log = l
	mit = device

	deviceID, _ = os.Hostname()

	if conf.LogLevel == "DEBUG" || conf.LogLevel == "debug" {
		log.Info("[http] Enabling pprof (LogLevel=DEBUG)")
		http.HandleFunc("/debug/pprof/", pprof.Index)
	}

	http.HandleFunc("/reboot", fnReboot)
	http.HandleFunc("/shutdown", fnShutdown)
	http.HandleFunc("/reset-ips", fnResetIPs)
	http.HandleFunc("/logs", fnLogs)
	http.HandleFunc("/backup", fnBackup)
	http.HandleFunc("/create-image", fnCreateImage)
	http.HandleFunc("/restore-image", fnRestoreImage)
	http.HandleFunc("/repair", fnRepair)
	http.HandleFunc("/", fnIndex)

	listen := fmt.Sprintf(":%d", conf.Port)
	log.Printf("[http] Local management server listening on %s", listen)

	go func() {
		if err := http.ListenAndServe(listen, nil); err != nil {
			log.Panicf("[http] Server exited: %s", err.Error())
		}
	}()
}

// maintainKey returns the maintenance key from mit.txt (or empty if not loaded).
func maintainKey() string {
	if mit != nil {
		return mit.MaintainKey
	}
	return ""
}

// ============================================================================

func fnIndex(w http.ResponseWriter, r *http.Request) {
	ba, err := os.ReadFile("html/index.html")
	if err != nil {
		log.Error("[http] index.html not found: ", err.Error())
		name := ""
		reg := ""
		if mit != nil {
			name = mit.DeviceName
			reg = mit.RegisterKey
		}
		w.Write([]byte(fmt.Sprintf("Device: %s | Name: %s | Register: %s", deviceID, name, reg)))
		return
	}

	out := strings.Replace(string(ba), "HHHH", deviceID, 1)
	if mit != nil {
		out = strings.Replace(out, "NNNN", mit.DeviceName, 1)
		out = strings.Replace(out, "RRRR", mit.RegisterKey, 1)
	}
	w.Write([]byte(out))
}

// ============================================================================

func fnReboot(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("mtnKey")
	log.Info("[http] Reboot requested")

	if key != maintainKey() || maintainKey() == "" {
		log.Error("[http] Invalid maintenance key for reboot")
		w.Write([]byte("Invalid key"))
		return
	}

	ba, err := os.ReadFile("html/reboot.html")
	if err != nil {
		w.Write([]byte("Rebooting..."))
	} else {
		w.Write(ba)
	}

	time.AfterFunc(2*time.Second, func() {
		runCommand("/usr/sbin/reboot")
	})
}

// ============================================================================

func fnShutdown(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("mtnKey")
	log.Info("[http] Shutdown requested")

	if key != maintainKey() || maintainKey() == "" {
		log.Error("[http] Invalid maintenance key for shutdown")
		w.Write([]byte("Invalid key"))
		return
	}

	ba, err := os.ReadFile("html/shutdown.html")
	if err != nil {
		w.Write([]byte("Shutting down..."))
	} else {
		w.Write(ba)
	}

	time.AfterFunc(2*time.Second, func() {
		runCommand("/usr/sbin/shutdown\t-h\tnow")
	})
}

// ============================================================================

func fnResetIPs(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("mtnKey")
	log.Info("[http] IP reset requested")

	if key != maintainKey() || maintainKey() == "" {
		log.Error("[http] Invalid maintenance key for reset-ips")
		w.Write([]byte("Invalid key"))
		return
	}

	ba, err := os.ReadFile("html/reset-ips.html")
	if err != nil {
		w.Write([]byte("Resetting IPs..."))
	} else {
		w.Write(ba)
	}

	go runCommand("scripts/reset_ips.sh")
}

// ============================================================================

func fnRepair(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("mtnKey")
	log.Info("[http] Filesystem repair requested")

	if key != maintainKey() || maintainKey() == "" {
		log.Error("[http] Invalid maintenance key for repair")
		w.Write([]byte("Invalid key"))
		return
	}

	ba, err := os.ReadFile("html/repair.html")
	if err != nil {
		w.Write([]byte("Starting repair..."))
	} else {
		w.Write(ba)
	}

	// maintenance_start.sh sets up the repair script as rc.local and reboots.
	// The device will boot into maintenance mode, run repair_fs.sh, then reboot normally.
	go func() {
		time.Sleep(2 * time.Second)
		out, code := runCommand("scripts/maintenance_start.sh\tscripts/repair_fs.sh")
		log.Printf("[http] maintenance_start exit=%d output=%s", code, out)
	}()
}

// ============================================================================

func fnLogs(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("mtnKey")
	logType := r.URL.Query().Get("logType")

	if key != maintainKey() || maintainKey() == "" {
		w.Write([]byte("Invalid key"))
		return
	}

	var out string
	var err error

	switch logType {
	case "docker":
		out, err = runCmd("docker", "service", "ls")
	case "container":
		svc := r.URL.Query().Get("service")
		if svc != "" {
			out, err = runCmd("docker", "service", "logs", "--tail=200", "--no-task-ids", "qube_"+svc)
		} else {
			out, err = runCmd("docker", "ps", "--format", "{{.Names}}\t{{.Status}}\t{{.Image}}")
		}
	default: // conf-agent
		out, err = runCmd("journalctl", "-u", "conf-agent", "-n", "200", "--no-pager")
		if err != nil {
			// fallback for Docker environment
			out = "Log streaming available via: docker logs <conf-agent-container>"
		}
	}

	if err != nil {
		w.Write([]byte(fmt.Sprintf("Error: %s\n%s", err.Error(), out)))
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(out))
}

// ============================================================================

func fnBackup(w http.ResponseWriter, r *http.Request) {
	// Data backup requires network share params — trigger via cloud command instead:
	// POST /api/v1/qubes/:id/commands  {"command":"backup_data","payload":{...}}
	w.Write([]byte("Use cloud API command 'backup_data' to trigger remote backup."))
}

func fnCreateImage(w http.ResponseWriter, r *http.Request) {
	// Image backup requires maintenance mode reboot — trigger via cloud command:
	// POST /api/v1/qubes/:id/commands  {"command":"backup_image"}
	w.Write([]byte("Use cloud API command 'backup_image' to trigger image backup."))
}

func fnRestoreImage(w http.ResponseWriter, r *http.Request) {
	// Image restore requires maintenance mode reboot — trigger via cloud command:
	// POST /api/v1/qubes/:id/commands  {"command":"restore_image"}
	w.Write([]byte("Use cloud API command 'restore_image' to trigger image restore."))
}

// ============================================================================
// Helpers
// ============================================================================

// runCommand executes a tab-separated command string (v1 compat format).
func runCommand(txt string) (string, int) {
	re := regexp.MustCompile(`[\t]+`)
	splts := re.Split(txt, -1)
	cmd := exec.Command(splts[0], splts[1:]...)

	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		}
		if code != 99 && code != 98 {
			log.Errorf("[http] Command error: %s — %s", txt, err.Error())
		}
	}
	return string(out), code
}

// runCmd executes a command with separate args and returns combined output.
func runCmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
