// Package docker handles Docker Swarm / Compose deployment for conf-agent.
// Detects whether the host is in Swarm mode and uses the appropriate deploy command.
package docker

import (
	"log"
	"os/exec"
	"path/filepath"
	"strings"
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
		log.Println("[docker] swarm mode — running: docker stack deploy")
		cmd := exec.Command("docker", "stack", "deploy",
			"-c", composePath, "--with-registry-auth", "qube")
		cmd.Dir = workDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("[docker] stack deploy FAILED: %v\n%s", err, out)
			return
		}
		log.Printf("[docker] stack deploy OK:\n%s", out)
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

// RestartService restarts a named service (reader container) on the Qube.
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
