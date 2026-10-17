package auth

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	reTunnelURL      = regexp.MustCompile(`https://[a-z0-9.-]+\.trycloudflare\.com`)
	solverMu         sync.Mutex
	ultimoSolverKick time.Time
)

// ExtrairUltimaTunnelURL lê o arquivo de log do túnel (ex: /home/ubuntu/tibidle-tunnel.log ou cloudflared.log)
// e retorna a URL pública ativa do trycloudflare.com
func ExtrairUltimaTunnelURL(logPath string) string {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return ""
	}
	matches := reTunnelURL.FindAllString(string(data), -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1]
}

// DispararSolverActions dispara o workflow ticket-relay no GitHub Actions em mode=push.
// O runner headless no GitHub resolve o Turnstile e devolve o token para a VPS via tunnel.
// Possui cooldown configurável (padrão 45s) para evitar sobrecarga de jobs simultâneos.
func DispararSolverActions(repo, workflowFile, tunnelLogPath, logID string, cooldown time.Duration) error {
	solverMu.Lock()
	if cooldown <= 0 {
		cooldown = 45 * time.Second
	}
	if time.Since(ultimoSolverKick) < cooldown {
		solverMu.Unlock()
		return fmt.Errorf("cooldown ativo (%v restante)", (cooldown - time.Since(ultimoSolverKick)).Round(time.Second))
	}
	ultimoSolverKick = time.Now()
	solverMu.Unlock()

	tunnel := ExtrairUltimaTunnelURL(tunnelLogPath)
	if tunnel == "" {
		return fmt.Errorf("[%s] nenhuma tunnel URL encontrada em %s", logID, tunnelLogPath)
	}

	// Verifica se o túnel está respondendo
	hc := &http.Client{Timeout: 5 * time.Second}
	resp, err := hc.Get(tunnel + "/api/status")
	if err != nil {
		return fmt.Errorf("[%s] tunnel %s indisponível: %w", logID, tunnel, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("[%s] tunnel %s respondeu HTTP %d", logID, tunnel, resp.StatusCode)
	}

	// Dispara o runner no GitHub via CLI nativo `gh`
	out, err := exec.Command("gh", "workflow", "run", workflowFile,
		"-R", repo,
		"-f", "mode=push",
		"-f", "push_url="+tunnel,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("[%s] gh workflow run falhou: %w (%s)", logID, err, strings.TrimSpace(string(out)))
	}

	log.Printf("[%s] ✔ Solver GitHub Actions disparado com sucesso: %s (push em %s)", logID, workflowFile, tunnel)
	return nil
}
