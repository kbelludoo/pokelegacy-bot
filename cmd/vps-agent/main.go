package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type SecretsConfig struct {
	TelegramToken  string `json:"telegramToken"`
	TelegramChatID string `json:"telegramChatId"`
}

func getTelegramConfig() (string, string) {
	token := os.Getenv("TELEGRAM_TOKEN")
	chatID := os.Getenv("TELEGRAM_CHAT_ID")
	if token != "" && chatID != "" {
		return token, chatID
	}

	homeDir, _ := os.UserHomeDir()
	candidatePaths := []string{
		".secrets.json",
		filepath.Join(homeDir, ".secrets.json"),
		"/home/ubuntu/.secrets.json",
	}

	for _, p := range candidatePaths {
		if data, err := os.ReadFile(p); err == nil {
			var cfg SecretsConfig
			if err := json.Unmarshal(data, &cfg); err == nil {
				if cfg.TelegramToken != "" && cfg.TelegramChatID != "" {
					return cfg.TelegramToken, cfg.TelegramChatID
				}
			}
		}
	}
	return "", ""
}

var (
	nickRegex        = regexp.MustCompile(`(?i)(l?kbelludoo\w*|lohmoreira|l?lrn\b|l?maumau\w*)`)
	emailRegex       = regexp.MustCompile(`(?i)[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	emailParenRegex  = regexp.MustCompile(`(?i)\([^)]*@[^)]*\)`)
	publicIPRegex    = regexp.MustCompile(`\b(129\.153|141\.148|193\.122)\.\d{1,3}\.\d{1,3}\b`)
	httpClient       = &http.Client{Timeout: 4 * time.Second}
)

type BotProbe struct {
	ID         string
	Name       string
	Port       int
	URL        string
	RestartCmd string
	Type       string
}

var allBots = []BotProbe{
	{ID: "huntera", Name: "⚔️ Huntera Idle", Port: 3000, URL: "http://127.0.0.1:3000/api/status", RestartCmd: "bash /home/ubuntu/huntera-go/start.sh", Type: "tmux"},
	{ID: "tibidle", Name: "🛡️ Tibidle Multi-Bot", Port: 3001, URL: "http://127.0.0.1:3001/api/status", RestartCmd: "sudo systemctl restart tibidle.service", Type: "systemd"},
	{ID: "sigma", Name: "⚡ Pokemon Sigma", Port: 3002, URL: "http://127.0.0.1:3002/api/status", RestartCmd: "sudo systemctl restart sigma-bot.service", Type: "systemd"},
	{ID: "leveling", Name: "🏹 Leveling Idle 2D", Port: 3003, URL: "http://127.0.0.1:3003/api/status", RestartCmd: "sudo systemctl restart leveling-bot.service", Type: "systemd"},
	{ID: "pokeidle", Name: "⚡ PokeIdle.io", Port: 3006, URL: "http://127.0.0.1:3006/api/status", RestartCmd: "sudo systemctl restart pokeidle.service", Type: "systemd"},
	{ID: "pokeidleworld", Name: "🌍 Poke Idle World", Port: 3007, URL: "http://127.0.0.1:3007/api/status", RestartCmd: "sudo systemctl restart pokeidleworld.service", Type: "systemd"},
	{ID: "kickminer", Name: "🟢 Kick Miner Go", Port: 3009, URL: "http://127.0.0.1:3009/api/status", RestartCmd: "sudo systemctl restart kick-miner.service", Type: "systemd"},
	{ID: "twitchminer", Name: "🟣 Twitch Miner Go", Port: 3010, URL: "http://127.0.0.1:3010/api/status", RestartCmd: "sudo systemctl restart twitch-miner.service", Type: "systemd"},
	{ID: "baiak", Name: "🏰 Baiak Idle Go", Port: 8090, URL: "http://127.0.0.1:8090/api/fleet", RestartCmd: "sudo systemctl restart baiak-fleet.service", Type: "systemd"},
	{ID: "lolidle", Name: "⚔️ LoL Idle Multi-Bot", Port: 3011, URL: "http://127.0.0.1:3011/api/status", RestartCmd: "sudo systemctl restart lolidle.service", Type: "systemd"},
}

// -------------------------------------------------------------
// TELEGRAM ALERTING
// -------------------------------------------------------------

func sendTelegram(text string) {
	token, chatID := getTelegramConfig()
	if token == "" || chatID == "" {
		return
	}
	payload := map[string]string{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

// -------------------------------------------------------------
// HTTP UTILITIES
// -------------------------------------------------------------

func fetchJSON(url string) interface{} {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var data interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil
	}
	return data
}

// -------------------------------------------------------------
// RECURSIVE STEALTH SCRUB
// -------------------------------------------------------------

func deepScrub(v interface{}) interface{} {
	switch val := v.(type) {
	case string:
		if strings.HasPrefix(val, "http://") || strings.HasPrefix(val, "https://") {
			return val
		}
		s := emailParenRegex.ReplaceAllString(val, "(Sessão Autenticada)")
		s = emailRegex.ReplaceAllString(s, "protegido")
		s = publicIPRegex.ReplaceAllString(s, "127.0.0.1")
		s = nickRegex.ReplaceAllString(s, "anonimo")
		return s
	case []interface{}:
		res := make([]interface{}, len(val))
		for i, elem := range val {
			res[i] = deepScrub(elem)
		}
		return res
	case map[string]interface{}:
		res := make(map[string]interface{})
		for k, elem := range val {
			cleanKey := k
			if !strings.HasPrefix(k, "http") {
				cleanKey = nickRegex.ReplaceAllString(cleanKey, "anonimo")
			}
			res[cleanKey] = deepScrub(elem)
		}
		return res
	default:
		return v
	}
}

// -------------------------------------------------------------
// GITHUB CONTENT PUSH
// -------------------------------------------------------------

func pushToGitHub(repo string, payload interface{}) error {
	scrubbed := deepScrub(payload)
	b, err := json.MarshalIndent(scrubbed, "", "  ")
	if err != nil {
		return err
	}

	b64 := base64.StdEncoding.EncodeToString(b)

	// Get SHA if exists
	shaCmd := exec.Command("gh", "api", fmt.Sprintf("repos/kbelludoo/%s/contents/docs/status.json", repo), "--jq", ".sha")
	shaOut, _ := shaCmd.Output()
	sha := strings.TrimSpace(string(shaOut))

	tmpPath := filepath.Join(os.TempDir(), fmt.Sprintf("status_%s.json", repo))
	reqBody := map[string]interface{}{
		"message": "telemetry: live bot status update (Go Stealth Engine)",
		"content": b64,
	}
	if sha != "" {
		reqBody["sha"] = sha
	}

	reqJSON, _ := json.Marshal(reqBody)
	if err := os.WriteFile(tmpPath, reqJSON, 0644); err != nil {
		return err
	}
	defer os.Remove(tmpPath)

	putCmd := exec.Command("gh", "api", "-X", "PUT", fmt.Sprintf("repos/kbelludoo/%s/contents/docs/status.json", repo), "--input", tmpPath)
	putCmd.Stdout = io.Discard
	putCmd.Stderr = io.Discard
	if err := putCmd.Run(); err != nil {
		return err
	}

	fmt.Printf("✓ [%s] Telemetria sincronizada com sucesso (Go Native)!\n", repo)
	return nil
}

// -------------------------------------------------------------
// SYSTEM METRICS (VPS)
// -------------------------------------------------------------

func getSystemMetrics() map[string]interface{} {
	metrics := map[string]interface{}{
		"uptime":        "N/A",
		"load":          "N/A",
		"ramTotalMb":    1024,
		"ramUsedMb":     512,
		"ramAvailableMb": 512,
		"ramPercent":    50,
		"swapTotalMb":   2048,
		"swapUsedMb":    0,
	}

	// 1. Meminfo
	if memData, err := os.ReadFile("/proc/meminfo"); err == nil {
		lines := strings.Split(string(memData), "\n")
		var totalKb, availKb, swapTotalKb, swapFreeKb int64
		for _, line := range lines {
			parts := strings.Fields(line)
			if len(parts) < 2 {
				continue
			}
			val, _ := strconv.ParseInt(parts[1], 10, 64)
			switch parts[0] {
			case "MemTotal:":
				totalKb = val
			case "MemAvailable:":
				availKb = val
			case "SwapTotal:":
				swapTotalKb = val
			case "SwapFree:":
				swapFreeKb = val
			}
		}
		if totalKb > 0 {
			totalMb := totalKb / 1024
			availMb := availKb / 1024
			usedMb := totalMb - availMb
			metrics["ramTotalMb"] = totalMb
			metrics["ramAvailableMb"] = availMb
			metrics["ramUsedMb"] = usedMb
			metrics["ramPercent"] = int(float64(usedMb) / float64(totalMb) * 100)
		}
		if swapTotalKb > 0 {
			metrics["swapTotalMb"] = swapTotalKb / 1024
			metrics["swapUsedMb"] = (swapTotalKb - swapFreeKb) / 1024
		}
	}

	// 2. Loadavg
	if loadData, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(loadData))
		if len(fields) >= 3 {
			metrics["load"] = strings.Join(fields[:3], " ")
		}
	}

	// 3. Uptime
	if out, err := exec.Command("uptime", "-p").Output(); err == nil {
		metrics["uptime"] = strings.TrimSpace(string(out))
	} else if upData, err := os.ReadFile("/proc/uptime"); err == nil {
		fields := strings.Fields(string(upData))
		if len(fields) > 0 {
			if secs, err := strconv.ParseFloat(fields[0], 64); err == nil {
				hours := int(secs) / 3600
				mins := (int(secs) % 3600) / 60
				metrics["uptime"] = fmt.Sprintf("up %d hours, %d minutes", hours, mins)
			}
		}
	}

	return metrics
}

// -------------------------------------------------------------
// HUNTERA INDEX IP SCRUB
// -------------------------------------------------------------

func sanitizeHunteraIndex() {
	shaCmd := exec.Command("gh", "api", "repos/kbelludoo/huntera-bot/contents/docs/index.html", "--jq", ".sha")
	shaOut, err := shaCmd.Output()
	if err != nil {
		return
	}
	sha := strings.TrimSpace(string(shaOut))
	if sha == "" {
		return
	}

	cntCmd := exec.Command("gh", "api", "repos/kbelludoo/huntera-bot/contents/docs/index.html", "--jq", ".content")
	cntOut, err := cntCmd.Output()
	if err != nil {
		return
	}
	rawB64 := strings.TrimSpace(string(cntOut))
	raw, err := base64.StdEncoding.DecodeString(rawB64)
	if err != nil {
		return
	}

	content := string(raw)
	if publicIPRegex.MatchString(content) {
		sanitized := publicIPRegex.ReplaceAllString(content, "127.0.0.1")
		b64 := base64.StdEncoding.EncodeToString([]byte(sanitized))
		tmpPath := filepath.Join(os.TempDir(), "huntera_index.json")
		reqBody := map[string]interface{}{
			"message": "security: sanitize raw VPS IP from index template (Go Native)",
			"content": b64,
			"sha":     sha,
		}
		data, _ := json.Marshal(reqBody)
		_ = os.WriteFile(tmpPath, data, 0644)
		defer os.Remove(tmpPath)

		_ = exec.Command("gh", "api", "-X", "PUT", "repos/kbelludoo/huntera-bot/contents/docs/index.html", "--input", tmpPath).Run()
		fmt.Println("✓ [huntera-bot] docs/index.html sanitizado via Go!")
	}
}

// -------------------------------------------------------------
// SYNC: INDIVIDUAL REPOSITORIES
// -------------------------------------------------------------

func syncIndividual() {
	sanitizeHunteraIndex()

	type individualTarget struct {
		repo  string
		port  int
		label string
	}

	targets := []individualTarget{
		{repo: "sigma-bot", port: 3002, label: "Treinador"},
		{repo: "tibidle-bot", port: 3001, label: "Conta"},
		{repo: "leveling-bot", port: 3003, label: "Party"},
		{repo: "pokeidle-go", port: 3006, label: "Treinador"},
		{repo: "pokeidleworld-bot", port: 3007, label: "Treinador"},
		{repo: "twitch-miner-go", port: 3010, label: "Minerador"},
		{repo: "lolidle-bot", port: 3011, label: "Campeão"},
	}

	for _, tgt := range targets {
		raw := fetchJSON(fmt.Sprintf("http://127.0.0.1:%d/api/status", tgt.port))
		if raw == nil {
			continue
		}

		m, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}

		// Especial para twitch-miner-go
		if tgt.repo == "twitch-miner-go" {
			if statuses, ok := m["statuses"].(map[string]interface{}); ok {
				safeStatuses := make(map[string]interface{})
				idx := 1
				for _, acc := range statuses {
					if accMap, ok := acc.(map[string]interface{}); ok {
						label := fmt.Sprintf("Minerador #%d", idx)
						idx++
						safeStatuses[label] = map[string]interface{}{
							"username":  label,
							"userId":    "anonimo",
							"streamers": accMap["streamers"],
						}
					}
				}
				m["statuses"] = safeStatuses
			}
			if logs, ok := m["logs"].([]interface{}); ok {
				safeLogs := make([]interface{}, len(logs))
				for i, l := range logs {
					if s, ok := l.(string); ok {
						safeLogs[i] = emailParenRegex.ReplaceAllString(nickRegex.ReplaceAllString(s, "Minerador #1"), "(Sessão Autenticada)")
					} else {
						safeLogs[i] = l
					}
				}
				m["logs"] = safeLogs
			}
		} else if accounts, ok := m["accounts"].([]interface{}); ok {
			for i, accRaw := range accounts {
				acc, ok := accRaw.(map[string]interface{})
				if !ok {
					continue
				}
				label := fmt.Sprintf("%s #%d", tgt.label, i+1)
				acc["id"] = fmt.Sprintf("acc-%d", i+1)

				if _, exists := acc["trainerName"]; exists {
					acc["trainerName"] = label
				}
				if _, exists := acc["alias"]; exists {
					acc["alias"] = label
				}
				if _, exists := acc["name"]; exists {
					acc["name"] = label
				}

				delete(acc, "bindIp")
				delete(acc, "outboundIp")

				if extra, ok := acc["extra"].(map[string]interface{}); ok {
					if _, exists := extra["trainerName"]; exists {
						extra["trainerName"] = label
					}
				}

				for _, logKey := range []string{"logs", "recentLogs"} {
					if logs, ok := acc[logKey].([]interface{}); ok {
						safeLogs := make([]interface{}, len(logs))
						for li, l := range logs {
							if s, ok := l.(string); ok {
								safeLogs[li] = emailParenRegex.ReplaceAllString(nickRegex.ReplaceAllString(s, label), "(Sessão Autenticada)")
							} else {
								safeLogs[li] = l
							}
						}
						acc[logKey] = safeLogs
					}
				}
			}
		}

		_ = pushToGitHub(tgt.repo, m)
	}
}

// -------------------------------------------------------------
// SYNC: HUB CENTRAL (bot-template-go)
// -------------------------------------------------------------

func syncHub() {
	vpsMetrics := getSystemMetrics()

	type botItem struct {
		name         string
		game         string
		engine       string
		port         int
		dashboardURL string
		status       interface{}
	}

	botDefs := map[string]botItem{
		"huntera":       {"Huntera Idle Bot", "Huntera", "Go Native Engine", 3000, "https://kbelludoo.github.io/huntera-bot/", nil},
		"tibidle":       {"Tibidle Multi-Bot", "Tibidle", "Go Native Engine", 3001, "https://kbelludoo.github.io/tibidle-bot/", nil},
		"sigma":         {"Pokemon Sigma Multi-Bot", "Pokemon Sigma", "Go Native Engine", 3002, "https://kbelludoo.github.io/sigma-bot/", nil},
		"leveling":      {"Leveling Idle 2D Bot", "Leveling Idle 2D", "Go Native Engine (Protobuf 3)", 3003, "https://kbelludoo.github.io/leveling-bot/", nil},
		"pokeidle":      {"PokeIdle.io Bot", "PokeIdle.io", "Go Native Engine (Twitch Lurker)", 3006, "https://kbelludoo.github.io/pokeidle-go/", nil},
		"pokeidleworld": {"Poke Idle World Bot", "Poke Idle World", "Go Native Engine (Auto-Catch)", 3007, "https://kbelludoo.github.io/pokeidleworld-bot/", nil},
		"kickminer":     {"Kick Channel Points Miner", "Kick.com", "Go Native (Zero-Video WS)", 3009, "https://kbelludoo.github.io/kick-miner-go/", nil},
		"twitchminer":   {"Twitch Channel Points Miner", "Twitch.tv", "Go Native (Zero-Video WS)", 3010, "https://kbelludoo.github.io/twitch-miner-go/", nil},
		"baiak":         {"Baiak Idle Multi-Account", "Baiak Idle", "Go Native Engine", 8090, "https://kbelludoo.github.io/baiak-idle-bot/", nil},
		"lolidle":       {"LoL Idle Multi-Bot", "LoL Idle", "Go Native Engine (Runeterra 4x Party)", 3011, "https://kbelludoo.github.io/lolidle-bot/", nil},
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	activeBotsCount := 0

	for k, def := range botDefs {
		wg.Add(1)
		go func(key string, d botItem) {
			defer wg.Done()
			var url string
			if key == "baiak" {
				url = fmt.Sprintf("http://127.0.0.1:%d/api/fleet", d.port)
			} else {
				url = fmt.Sprintf("http://127.0.0.1:%d/api/status", d.port)
			}
			st := fetchJSON(url)

			mu.Lock()
			d.status = st
			botDefs[key] = d
			if st != nil {
				activeBotsCount++
			}
			mu.Unlock()
		}(k, def)
	}
	wg.Wait()

	// Mask payloads
	botsPayload := make(map[string]interface{})
	for k, def := range botDefs {
		botMap := map[string]interface{}{
			"name":         def.name,
			"game":         def.game,
			"engine":       def.engine,
			"port":         def.port,
			"dashboardUrl": def.dashboardURL,
			"status":       def.status,
		}

		if def.status != nil {
			if stMap, ok := def.status.(map[string]interface{}); ok {
				// Sanitização específica por bot
				switch k {
				case "huntera":
					if accs, ok := stMap["accounts"].([]interface{}); ok {
						for i, a := range accs {
							if am, ok := a.(map[string]interface{}); ok {
								am["charName"] = fmt.Sprintf("Personagem #%d", i+1)
								am["id"] = fmt.Sprintf("acc%d", i+1)
							}
						}
					}
				case "tibidle":
					if accs, ok := stMap["accounts"].([]interface{}); ok {
						for i, a := range accs {
							if am, ok := a.(map[string]interface{}); ok {
								am["name"] = fmt.Sprintf("Conta #%d", i+1)
								am["id"] = fmt.Sprintf("acc%d", i+1)
								if logs, ok := am["recentLogs"].([]interface{}); ok {
									safeLogs := make([]interface{}, len(logs))
									for li, l := range logs {
										if s, ok := l.(string); ok {
											safeLogs[li] = emailParenRegex.ReplaceAllString(s, "(Sessão Autenticada)")
										} else {
											safeLogs[li] = l
										}
									}
									am["recentLogs"] = safeLogs
								}
							}
						}
					}
				case "sigma":
					if accs, ok := stMap["accounts"].([]interface{}); ok {
						for i, a := range accs {
							if am, ok := a.(map[string]interface{}); ok {
								am["trainerName"] = fmt.Sprintf("Treinador #%d", i+1)
								am["alias"] = fmt.Sprintf("Treinador #%d", i+1)
								am["id"] = fmt.Sprintf("acc%d", i+1)
								delete(am, "bindIp")
								delete(am, "outboundIp")
							}
						}
					}
				case "leveling":
					if accs, ok := stMap["accounts"].([]interface{}); ok {
						for i, a := range accs {
							if am, ok := a.(map[string]interface{}); ok {
								am["name"] = fmt.Sprintf("Party #%d", i+1)
								am["id"] = fmt.Sprintf("acc-%d", i+1)
							}
						}
					}
				case "pokeidle", "pokeidleworld":
					if accs, ok := stMap["accounts"].([]interface{}); ok {
						for i, a := range accs {
							if am, ok := a.(map[string]interface{}); ok {
								am["name"] = fmt.Sprintf("Treinador #%d", i+1)
								am["id"] = fmt.Sprintf("acc-%d", i+1)
								if extra, ok := am["extra"].(map[string]interface{}); ok {
									extra["trainerName"] = fmt.Sprintf("Treinador #%d", i+1)
								}
							}
						}
					}
				case "kickminer":
					if accs, ok := stMap["accounts"].(map[string]interface{}); ok {
						safeAccs := make(map[string]interface{})
						idx := 1
						for _, item := range accs {
							if im, ok := item.(map[string]interface{}); ok {
								label := fmt.Sprintf("Minerador #%d", idx)
								im["username"] = label
								safeAccs[fmt.Sprintf("miner_%d", idx)] = im
								idx++
							}
						}
						stMap["accounts"] = safeAccs
					}
				case "twitchminer":
					if statuses, ok := stMap["statuses"].(map[string]interface{}); ok {
						safeStatuses := make(map[string]interface{})
						idx := 1
						for _, item := range statuses {
							if im, ok := item.(map[string]interface{}); ok {
								label := fmt.Sprintf("Minerador #%d", idx)
								safeStatuses[label] = map[string]interface{}{
									"username":  label,
									"userId":    "anonimo",
									"streamers": im["streamers"],
								}
								idx++
							}
						}
						stMap["statuses"] = safeStatuses
					}
					if logs, ok := stMap["logs"].([]interface{}); ok {
						safeLogs := make([]interface{}, len(logs))
						for li, l := range logs {
							if s, ok := l.(string); ok {
								safeLogs[li] = emailParenRegex.ReplaceAllString(nickRegex.ReplaceAllString(s, "Minerador #1"), "(Sessão Autenticada)")
							} else {
								safeLogs[li] = l
							}
						}
						stMap["logs"] = safeLogs
					}
				case "baiak":
					if botsMap, ok := stMap["bots"].(map[string]interface{}); ok {
						keys := make([]string, 0, len(botsMap))
						for k := range botsMap {
							keys = append(keys, k)
						}
						sort.Strings(keys)
						for idx, k := range keys {
							if bm, ok := botsMap[k].(map[string]interface{}); ok {
								bm["name"] = ""
								bm["label"] = fmt.Sprintf("Conta %d", idx+1)
							}
						}
					}
				case "lolidle":
					if accs, ok := stMap["accounts"].([]interface{}); ok {
						for i, a := range accs {
							if am, ok := a.(map[string]interface{}); ok {
								label := fmt.Sprintf("Campeão #%d", i+1)
								am["name"] = label
								am["id"] = fmt.Sprintf("acc-%d", i+1)
								delete(am, "bindIp")
								delete(am, "outboundIp")
								if logs, ok := am["logs"].([]interface{}); ok {
									safeLogs := make([]interface{}, len(logs))
									for li, l := range logs {
										if s, ok := l.(string); ok {
											safeLogs[li] = emailParenRegex.ReplaceAllString(nickRegex.ReplaceAllString(s, label), "(Sessão Autenticada)")
										} else {
											safeLogs[li] = l
										}
									}
									am["logs"] = safeLogs
								}
							}
						}
					}
				}
			}
		}

		botsPayload[k] = botMap
	}

	payload := map[string]interface{}{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"vps":       vpsMetrics,
		"summary": map[string]interface{}{
			"totalBots":  len(botDefs),
			"activeBots": activeBotsCount,
		},
		"bots": botsPayload,
	}

	_ = pushToGitHub("bot-template-go", payload)
}

// -------------------------------------------------------------
// WATCHDOG HEALTHCHECK & AUTO-HEAL
// -------------------------------------------------------------

var incidentHistory = make(map[string]int64)

func runWatchdog() {
	fmt.Printf("[watchdog-go] Verificando integridade dos %d bots... (%s)\n", len(allBots), time.Now().Format("15:04:05"))

	for _, bot := range allBots {
		resp, err := httpClient.Get(bot.URL)
		isHealthy := err == nil && resp.StatusCode == http.StatusOK
		if resp != nil {
			resp.Body.Close()
		}

		if isHealthy {
			delete(incidentHistory, bot.ID)
			continue
		}

		fmt.Printf("[watchdog-go] ⚠️ ALERTA: %s (Porta %d) FORA DO AR! Tentando auto-cura...\n", bot.Name, bot.Port)

		// Executa comando de recuperação
		parts := strings.Fields(bot.RestartCmd)
		if len(parts) > 0 {
			cmd := exec.Command(parts[0], parts[1:]...)
			_ = cmd.Run()
		}

		time.Sleep(6 * time.Second)

		// Reteste
		reResp, reErr := httpClient.Get(bot.URL)
		reHealthy := reErr == nil && reResp.StatusCode == http.StatusOK
		if reResp != nil {
			reResp.Body.Close()
		}

		if reHealthy {
			fmt.Printf("[watchdog-go] ✅ Auto-cura concluída para %s (Porta %d)!\n", bot.Name, bot.Port)
			msg := fmt.Sprintf("🛠️ *AUTO-CURA CONCLUÍDA NA VPS (Go Native)*\n\n"+
				"• *Bot:* %s\n"+
				"• *Porta:* `%d`\n"+
				"• *Ação:* Serviço reiniciado automaticamente pelo Watchdog em Go.\n"+
				"• *Status Atual:* ✅ ONLINE e operando normalmente!", bot.Name, bot.Port)
			sendTelegram(msg)
			delete(incidentHistory, bot.ID)
		} else {
			fmt.Printf("[watchdog-go] ❌ Falha na auto-cura de %s. Enviando alerta crítico...\n", bot.Name)
			now := time.Now().UnixMilli()
			lastAlert := incidentHistory[bot.ID]
			if lastAlert == 0 || (now-lastAlert > 600000) {
				incidentHistory[bot.ID] = now
				msg := fmt.Sprintf("🚨 *ALERTA CRÍTICO: BOT FORA DO AR!*\n\n"+
					"• *Bot:* %s\n"+
					"• *Porta:* `%d`\n"+
					"• *Tentativa de Auto-Cura:* ❌ Falhou ao reiniciar\n"+
					"• *Verificação manual:* `sudo systemctl status %s`", bot.Name, bot.Port, bot.ID)
				sendTelegram(msg)
			}
		}
	}
}

// -------------------------------------------------------------
// MAIN ENTRYPOINT
// -------------------------------------------------------------

func main() {
	allFlag := flag.Bool("all", false, "Executa sync do hub e repositórios individuais")
	hubFlag := flag.Bool("hub", false, "Executa sincronização do Bot Command Center Hub")
	indFlag := flag.Bool("individual", false, "Executa sincronização dos repositórios individuais")
	watchdogFlag := flag.Bool("watchdog", false, "Executa ciclo do Watchdog de auto-cura")

	flag.Parse()

	if *watchdogFlag {
		runWatchdog()
		return
	}

	if *allFlag {
		fmt.Printf("[%s] Iniciando ciclo completo de sincronização em Go Native...\n", time.Now().Format("15:04:05"))
		syncHub()
		syncIndividual()
		fmt.Println("✓ Ciclo completo de telemetria Go Native concluído com sucesso!")
		return
	}

	if *hubFlag {
		syncHub()
		return
	}

	if *indFlag {
		syncIndividual()
		return
	}

	// Padrão se chamado sem flags: executa sync completo
	syncHub()
	syncIndividual()
}
