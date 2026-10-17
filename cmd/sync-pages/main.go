package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"time"

	"bot-template-go/pkg/ghsync"
	"bot-template-go/pkg/stealth"
)

var reConectado = regexp.MustCompile(`Conectado como [^)]*\)`)

func fetchStatus(port int) any {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/api/status", port))
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	parsed, err := stealth.Decode(data)
	if err != nil {
		return nil
	}
	if !stealth.Truthy(parsed) {
		return nil
	}
	return parsed
}

func hubScrub(v any) any {
	return stealth.Hub().Apply(v)
}

func accountsArray(m map[string]any) ([]any, bool) {
	accs, ok := m["accounts"].([]any)
	return accs, ok
}

func sanitizeGeneric(status map[string]any) {
	accs, ok := accountsArray(status)
	if !ok {
		return
	}
	for i, a := range accs {
		acc, ok := a.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := acc["name"].(string); ok {
			acc["name"] = fmt.Sprintf("Conta #%d", i+1)
		}
		if _, ok := acc["trainerName"].(string); ok {
			acc["trainerName"] = fmt.Sprintf("Treinador #%d", i+1)
		}
		if _, ok := acc["alias"].(string); ok {
			acc["alias"] = fmt.Sprintf("Treinador #%d", i+1)
		}
		if _, ok := acc["charName"].(string); ok {
			acc["charName"] = fmt.Sprintf("Personagem #%d", i+1)
		}
		delete(acc, "bindIp")
		delete(acc, "outboundIp")
	}
}

func sanitizeSigma(status map[string]any) {
	accs, ok := accountsArray(status)
	if !ok {
		return
	}
	for i, a := range accs {
		acc, ok := a.(map[string]any)
		if !ok {
			continue
		}
		acc["id"] = fmt.Sprintf("acc%d", i+1)
		acc["trainerName"] = fmt.Sprintf("Treinador #%d", i+1)
		acc["alias"] = fmt.Sprintf("Treinador #%d", i+1)
		delete(acc, "bindIp")
		delete(acc, "outboundIp")
	}
}

func sanitizeTibidle(status map[string]any) {
	accs, ok := accountsArray(status)
	if !ok {
		return
	}
	for i, a := range accs {
		acc, ok := a.(map[string]any)
		if !ok {
			continue
		}
		acc["id"] = fmt.Sprintf("acc%d", i+1)
		acc["name"] = fmt.Sprintf("Conta #%d", i+1)
		if logs, ok := acc["recentLogs"].([]any); ok {
			with := fmt.Sprintf("Conectado como Conta #%d", i+1)
			for j, l := range logs {
				if s, ok := l.(string); ok {
					logs[j] = reConectado.ReplaceAllLiteralString(s, with)
				}
			}
		}
	}
}

func sanitizeLeveling(status map[string]any) {
	accs, ok := accountsArray(status)
	if !ok {
		return
	}
	for i, a := range accs {
		acc, ok := a.(map[string]any)
		if !ok {
			continue
		}
		acc["id"] = fmt.Sprintf("acc-%d", i+1)
		acc["name"] = fmt.Sprintf("Party #%d", i+1)
		if logs, ok := acc["logs"].([]any); ok {
			with := fmt.Sprintf("Party #%d", i+1)
			for j, l := range logs {
				if s, ok := l.(string); ok {
					logs[j] = stealth.NickRE.ReplaceAllLiteralString(s, with)
				}
			}
		}
	}
}

func tunnelURL(logPath string) string {
	raw, err := os.ReadFile(logPath)
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`https://[a-zA-Z0-9.-]*\.trycloudflare\.com`)
	matches := re.FindAllString(string(raw), -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1]
}

func pushGH(repo string, payload any) {
	jsonBytes, err := stealth.Marshal(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] Falha ao serializar: %v\n", err)
		return
	}
	sha, err := ghsync.FileSHA(repo, "docs/status.json")
	if err != nil {
		sha = ""
	}
	if err := ghsync.Put(repo, "docs/status.json", "telemetry: live bot status update (stealth)", jsonBytes, sha); err != nil {
		fmt.Fprintf(os.Stderr, "[-] Falha ao sincronizar %s: %v\n", repo, err)
		return
	}
	fmt.Println("Telemetria anonimizada sincronizada com o GitHub Pages!")
}

func writeLocalAndGit(payload any) {
	docsDir := "docs"
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "[-] Falha ao criar docs/: %v\n", err)
		return
	}
	jsonBytes, err := stealth.Marshal(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] Falha ao serializar: %v\n", err)
		return
	}
	tmp := docsDir + "/status.json.tmp"
	if err := os.WriteFile(tmp, jsonBytes, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "[-] Falha ao escrever tmp: %v\n", err)
		return
	}
	if err := os.Rename(tmp, docsDir+"/status.json"); err != nil {
		fmt.Fprintf(os.Stderr, "[-] Falha ao mover tmp: %v\n", err)
		return
	}
	if _, err := os.Stat(".git"); err != nil {
		fmt.Println("Telemetria anonimizada sincronizada com o GitHub Pages!")
		return
	}
	run := func(name string, args ...string) {
		cmd := exec.Command(name, args...)
		cmd.Stdout = nil
		cmd.Stderr = nil
		cmd.Run()
	}
	run("git", "add", docsDir+"/status.json")
	commitMsg := fmt.Sprintf("chore: sync telemetry %s", time.Now().Format("2006-01-02 15:04:05"))
	run("git", "commit", "-m", commitMsg)
	run("git", "push", "origin", "main")
	fmt.Println("Telemetria anonimizada sincronizada com o GitHub Pages!")
}

func main() {
	mode := "generic"
	port := 3001
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	if len(os.Args) > 2 {
		fmt.Sscanf(os.Args[2], "%d", &port)
	}

	switch mode {
	case "sigma":
		status := fetchStatus(3002)
		if status == nil {
			return
		}
		if m, ok := status.(map[string]any); ok {
			sanitizeSigma(m)
			pushGH("sigma-bot", hubScrub(m))
		}
	case "tibidle":
		status := fetchStatus(3001)
		if status == nil {
			return
		}
		if m, ok := status.(map[string]any); ok {
			sanitizeTibidle(m)
			if u := tunnelURL("/home/ubuntu/tibidle-tunnel.log"); u != "" {
				m["tunnelUrl"] = u
			}
			pushGH("tibidle-bot", hubScrub(m))
		}
	case "leveling":
		status := fetchStatus(3003)
		if status == nil {
			return
		}
		if m, ok := status.(map[string]any); ok {
			sanitizeLeveling(m)
			pushGH("leveling-bot", hubScrub(m))
		}
	default:
		status := fetchStatus(port)
		if status == nil {
			return
		}
		if m, ok := status.(map[string]any); ok {
			sanitizeGeneric(m)
			writeLocalAndGit(hubScrub(m))
		}
	}
}
