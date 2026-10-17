package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"time"

	"bot-template-go/pkg/ghsync"
	"bot-template-go/pkg/stealth"
)

var (
	reIDLogin = regexp.MustCompile(`(?i)\(ID:\s*\d+,\s*Login:\s*[^)]+\)`)
)

type botDef struct {
	repo   string
	port   int
	label  string
	fields []string
}

var bots = []botDef{
	{repo: "sigma-bot", port: 3002, label: "Treinador", fields: []string{"trainerName", "alias"}},
	{repo: "tibidle-bot", port: 3001, label: "Conta", fields: []string{"name"}},
	{repo: "leveling-bot", port: 3003, label: "Party", fields: []string{"name"}},
	{repo: "pokeidle-go", port: 3006, label: "Treinador", fields: []string{"name"}},
	{repo: "pokeidleworld-bot", port: 3007, label: "Treinador", fields: []string{"name"}},
	{repo: "twitch-miner-go", port: 3010, label: "Minerador", fields: []string{"username"}},
}

func fetchJSON(url string) any {
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get(url)
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
	return parsed
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func anonymizeAccounts(payload any, bot botDef) any {
	if payload == nil {
		return nil
	}
	m, ok := payload.(map[string]any)
	if !ok {
		return payload
	}

	if bot.repo == "twitch-miner-go" {
		if stMap, ok := m["statuses"].(map[string]any); ok {
			safe := make(map[string]any, len(stMap))
			for i, u := range sortedKeys(stMap) {
				acc, _ := stMap[u].(map[string]any)
				label := fmt.Sprintf("Minerador #%d", i+1)
				streamers := any(map[string]any{})
				if acc != nil {
					if s, ok := acc["streamers"]; ok && s != nil {
						streamers = s
					}
				}
				safe[label] = map[string]any{
					"username":  label,
					"userId":    "anonimo",
					"streamers": streamers,
				}
			}
			m["statuses"] = safe
		}
		if logs, ok := m["logs"].([]any); ok {
			for i, l := range logs {
				if s, ok := l.(string); ok {
					s = stealth.NickRE.ReplaceAllLiteralString(s, "Minerador #1")
					s = reIDLogin.ReplaceAllLiteralString(s, "(Sessão Autenticada)")
					logs[i] = s
				}
			}
		}
		return m
	}

	accs, ok := m["accounts"].([]any)
	if !ok {
		return m
	}
	for i, a := range accs {
		acc, ok := a.(map[string]any)
		if !ok {
			continue
		}
		label := fmt.Sprintf("%s #%d", bot.label, i+1)
		for _, f := range bot.fields {
			if _, ok := acc[f].(string); ok {
				acc[f] = label
			}
		}
		if _, ok := acc["id"].(string); ok {
			acc["id"] = fmt.Sprintf("acc-%d", i+1)
		}
		if extra, ok := acc["extra"].(map[string]any); ok {
			if _, ok := extra["trainerName"].(string); ok {
				extra["trainerName"] = label
			}
		}
		for _, k := range []string{"logs", "recentLogs"} {
			logs, ok := acc[k].([]any)
			if !ok {
				continue
			}
			for j, l := range logs {
				s, ok := l.(string)
				if !ok {
					continue
				}
				s = stealth.SessionRE.ReplaceAllLiteralString(s, "(Sessão Autenticada)")
				s = stealth.EmailRE.ReplaceAllLiteralString(s, "protegido")
				s = stealth.NickRE.ReplaceAllLiteralString(s, label)
				logs[j] = s
			}
		}
		if stealth.Truthy(acc["bindIp"]) {
			acc["bindIp"] = "127.0.0.1"
		}
		if stealth.Truthy(acc["outboundIp"]) {
			acc["outboundIp"] = "127.0.0.1"
		}
	}
	return m
}

func pushToGH(repo string, payload any) {
	safe := stealth.Individual().Apply(payload)
	jsonBytes, err := stealth.Marshal(safe)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] [%s] Erro ao serializar: %v\n", repo, err)
		return
	}
	sha, err := ghsync.FileSHA(repo, "docs/status.json")
	if err != nil {
		sha = ""
	}
	if err := ghsync.Put(repo, "docs/status.json", "telemetry: live bot status update (stealth)", jsonBytes, sha); err != nil {
		fmt.Fprintf(os.Stderr, "[-] [%s] Erro ao sincronizar: %v\n", repo, err)
		return
	}
	fmt.Printf("✓ [%s] Telemetria anonimizada sincronizada com sucesso!\n", repo)
}

func sanitizeHunteraIndex() {
	const repo = "huntera-bot"
	sha, err := ghsync.FileSHA(repo, "docs/index.html")
	if err != nil || sha == "" {
		return
	}
	raw, err := ghsync.Content(repo, "docs/index.html")
	if err != nil {
		return
	}
	reIP := regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`)
	if !reIP.Match(raw) {
		return
	}
	sanitized := reIP.ReplaceAllFunc(raw, func(ip []byte) []byte {
		s := string(ip)
		if len(s) >= 4 && s[:4] == "127." || s == "0.0.0.0" {
			return ip
		}
		return []byte("127.0.0.1")
	})
	if err := ghsync.Put(repo, "docs/index.html", "security: sanitize raw VPS IP from index template", sanitized, sha); err != nil {
		return
	}
	fmt.Printf("✓ [%s] docs/index.html sanitizado (IP removido)!\n", repo)
}

func main() {
	sanitizeHunteraIndex()
	for _, bot := range bots {
		status := fetchJSON(fmt.Sprintf("http://127.0.0.1:%d/api/status", bot.port))
		if status == nil {
			continue
		}
		pushToGH(bot.repo, anonymizeAccounts(status, bot))
	}
}
