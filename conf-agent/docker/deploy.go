// Package docker handles Docker Swarm / Compose deployment for conf-agent.
// Detects whether the host is in Swarm mode and uses the appropriate deploy command.
package docker

import (
	"log"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Deploy writes and deploys the docker-compose.yml to Docker.
// Uses `docker stack deploy` in Swarm mode, `docker compose up -d` otherwise.
func Deploy(workDir string) {
	if _, err := exec.LookPath("docker"); err != nil {
		log.Println("[docker] docker not in PATH — skipping deploy (test mode)")
		return
	}

	composePath := filepath.Join(workDir, "docker-compose.yml")
	swarmOut, _ := exec.Command("docker", "info", "--format", "{{.Swarm.LocalNodeState}}").Output()
	isSwarm := strings.TrimSpace(string(swarmOut)) == "active"

	if isSwarm {
		var out []byte
		var err error
		// Retry up to 3 times to handle "update out of sequence" race conditions in Swarm
		for i := 0; i < 3; i++ {
			log.Printf("[docker] swarm mode — running: docker stack deploy (attempt %d/3)", i+1)
			cmd := exec.Command("docker", "stack", "deploy",
				"-c", composePath, "--with-registry-auth", "--prune", "qube")
			cmd.Dir = workDir
			out, err = cmd.CombinedOutput()
			if err == nil {
				log.Printf("[docker] stack deploy OK:\n%s", out)
				return
			}

			if strings.Contains(string(out), "update out of sequence") {
				backoff := time.Duration(i+1) * 2 * time.Second
				log.Printf("[docker] stack deploy FAILED: update out of sequence. retrying in %v...", backoff)
				time.Sleep(backoff)
				continue
			}
			break
		}
		log.Printf("[docker] stack deploy FAILED: %v\n%s", err, out)
	} else {
		log.Println("[docker] compose mode — running: docker compose up -d")
		cmd := exec.Command("docker", "compose", "-f", composePath,
			"up", "-d", "--remove-orphans")
		cmd.Dir = workDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("[docker] compose deploy FAILED: %v\n%s", err, out)
			return
		}
		log.Printf("[docker] compose deploy OK:\n%s", out)
	}
}

// RestartService restarts a named reader service on the Qube.
func RestartService(service, workDir string) (string, error) {
	swarmOut, _ := exec.Command("docker", "info", "--format", "{{.Swarm.LocalNodeState}}").Output()
	isSwarm := strings.TrimSpace(string(swarmOut)) == "active"

	if isSwarm {
		return Run("docker", "service", "update", "--force", "qube_"+service)
	}
	return Run("docker", "compose", "-f",
		filepath.Join(workDir, "docker-compose.yml"), "restart", service)
}

// Run executes a command and returns its combined output.
func Run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
