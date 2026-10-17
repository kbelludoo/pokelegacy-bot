package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"bot-template-go/pkg/stealth"
)

type secretsConfig struct {
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
			var cfg secretsConfig
			if err := json.Unmarshal(data, &cfg); err == nil {
				if cfg.TelegramToken != "" && cfg.TelegramChatID != "" {
					return cfg.TelegramToken, cfg.TelegramChatID
				}
			}
		}
	}
	return "", ""
}

type botConfig struct {
	id         string
	name       string
	port       int
	checkURL   string
	restartCmd string
}

var botsConfig = []botConfig{
	{id: "huntera", name: "⚔️ Huntera Idle", port: 3000, checkURL: "http://127.0.0.1:3000/api/status", restartCmd: "bash /home/ubuntu/huntera-go/start.sh"},
	{id: "tibidle", name: "🛡️ Tibidle Multi-Bot", port: 3001, checkURL: "http://127.0.0.1:3001/api/status", restartCmd: "sudo systemctl restart tibidle.service"},
	{id: "sigma", name: "⚡ Pokemon Sigma", port: 3002, checkURL: "http://127.0.0.1:3002/api/status", restartCmd: "sudo systemctl restart sigma-bot.service"},
	{id: "leveling", name: "🏹 Leveling Idle 2D", port: 3003, checkURL: "http://127.0.0.1:3003/api/status", restartCmd: "sudo systemctl restart leveling-bot.service"},
	{id: "pokeidle", name: "⚡ PokeIdle.io", port: 3006, checkURL: "http://127.0.0.1:3006/api/status", restartCmd: "sudo systemctl restart pokeidle.service"},
	{id: "pokeidleworld", name: "🌍 Poke Idle World", port: 3007, checkURL: "http://127.0.0.1:3007/api/status", restartCmd: "sudo systemctl restart pokeidleworld.service"},
	{id: "kickminer", name: "🟢 Kick Miner Go", port: 3009, checkURL: "http://127.0.0.1:3009/api/status", restartCmd: "sudo systemctl restart kick-miner.service"},
	{id: "twitchminer", name: "🟣 Twitch Miner Go", port: 3010, checkURL: "http://127.0.0.1:3010/api/status", restartCmd: "sudo systemctl restart twitch-miner.service"},
	{id: "baiak", name: "🏰 Baiak Idle Go", port: 8090, checkURL: "http://127.0.0.1:8090/api/fleet", restartCmd: "sudo systemctl restart baiak-fleet.service"},
}

type probeResult struct {
	ok    bool
	data  any
	error string
}

var incidentHistory = map[string]time.Time{}

func sendTelegram(text string) bool {
	token, chatID := getTelegramConfig()
	if token == "" || chatID == "" {
		return false
	}
	payload, err := json.Marshal(map[string]string{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] Erro sendTelegram: %v\n", err)
		return false
	}
	client := &http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequest(http.MethodPost, "https://api.telegram.org/bot"+token+"/sendMessage", bytes.NewReader(payload))
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] Falha ao enviar telegram: %v\n", err)
		return false
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[-] Falha ao enviar telegram: %v\n", err)
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return true
}

func probePort(url string, timeoutMs int) probeResult {
	client := &http.Client{Timeout: time.Duration(timeoutMs) * time.Millisecond}
	resp, err := client.Get(url)
	if err != nil {
		msg := err.Error()
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			msg = "Timeout"
		}
		return probeResult{ok: false, error: msg}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return probeResult{ok: false, error: err.Error()}
	}
	parsed, err := stealth.Decode(data)
	if err != nil {
		return probeResult{ok: resp.StatusCode == 200, data: nil}
	}
	return probeResult{ok: true, data: parsed}
}

func askGoAgent(prompt string) any {
	payload, err := json.Marshal(map[string]string{"message": prompt})
	if err != nil {
		return nil
	}
	client := &http.Client{Timeout: 45 * time.Second}
	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:8888/agent", bytes.NewReader(payload))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	j, err := stealth.Decode(data)
	if err != nil {
		return string(data)
	}
	m, ok := j.(map[string]any)
	if !ok {
		return string(data)
	}
	if r, ok := m["response"]; ok && r != nil {
		return r
	}
	if e, ok := m["error"]; ok && e != nil {
		return e
	}
	return string(data)
}

func runRestart(cmdStr string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", cmdStr)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

func runWatchdogCycle() {
	fmt.Printf("[watchdog] Verificando integridade dos 8 bots... (%s)\n", time.Now().Format("15:04:05"))

	for _, bot := range botsConfig {
		probe := probePort(bot.checkURL, 3000)

		isHealthy := probe.ok
		if bot.id == "baiak" && probe.ok {
			if fleet, ok := probe.data.(map[string]any); ok {
				if f, ok := fleet["fleet"].(map[string]any); ok {
					if stealth.Truthy(f["botsOnline"]) {
						if n, ok := f["botsOnline"].(json.Number); ok {
							if fv, _ := n.Float64(); fv == 0 {
								isHealthy = false
							}
						} else if n, ok := f["botsOnline"].(float64); ok && n == 0 {
							isHealthy = false
						}
					} else {
						isHealthy = false
					}
				}
			}
		}

		if isHealthy {
			if _, ok := incidentHistory[bot.id]; ok {
				fmt.Printf("[watchdog] Bot %s normalizou.\n", bot.name)
				delete(incidentHistory, bot.id)
			}
			continue
		}

		errMsg := probe.error
		if errMsg == "" {
			errMsg = "sem resposta"
		}
		fmt.Printf("[watchdog] ⚠️ ALERTA: %s (Porta %d) está FORA DO AR! (%s)\n", bot.name, bot.port, errMsg)

		fmt.Printf("[watchdog] 🔧 Executando auto-cura para %s: %s\n", bot.name, bot.restartCmd)
		if err := runRestart(bot.restartCmd); err != nil {
			fmt.Fprintf(os.Stderr, "[-] Falha ao executar comando de auto-cura para %s: %v\n", bot.name, err)
		}

		time.Sleep(6 * time.Second)

		retest := probePort(bot.checkURL, 4000)

		if retest.ok {
			fmt.Printf("[watchdog] ✅ Auto-cura bem-sucedida! %s voltou a responder na porta %d.\n", bot.name, bot.port)
			msg := fmt.Sprintf(
				"🛠️ *AUTO-CURA CONCLUÍDA NA VPS*\n\n• *Bot:* %s\n• *Porta:* `%d`\n• *Ação:* Serviço reiniciado automaticamente pelo Watchdog.\n• *Status Atual:* ✅ ONLINE e operando normalmente!",
				bot.name, bot.port,
			)
			sendTelegram(msg)
			delete(incidentHistory, bot.id)
		} else {
			fmt.Fprintf(os.Stderr, "[watchdog] ❌ Auto-cura falhou para %s. Acionando Agent Go para diagnóstico...\n", bot.name)

			agentPrompt := fmt.Sprintf(
				"O bot %s na porta %d caiu e a reinicializacao rapida falhou. Verifique os ultimos logs do systemd ou processos, explique por que ele nao esta subindo e o que precisa ser feito.",
				bot.name, bot.port,
			)
			agentReport := askGoAgent(agentPrompt)

			var reportClean string
			if agentReport != nil {
				s := fmt.Sprintf("%v", agentReport)
				if len(s) > 800 {
					s = s[:800]
				}
				reportClean = s
			} else {
				reportClean = fmt.Sprintf("Erro ao obter resposta do Agent Go. Verifique manualmente via SSH: `sudo systemctl status %s`", bot.id)
			}

			now := time.Now()
			last, hasIncident := incidentHistory[bot.id]
			if !hasIncident || now.Sub(last) > 10*time.Minute {
				incidentHistory[bot.id] = now
				msg := fmt.Sprintf(
					"🚨 *ALERTA CRÍTICO: BOT FORA DO AR!*\n\n• *Bot:* %s\n• *Porta:* `%d`\n• *Tentativa de Auto-Cura:* ❌ Falhou ao reiniciar\n\n🤖 *Diagnóstico do Agent Go:*\n%s",
					bot.name, bot.port, reportClean,
				)
				sendTelegram(msg)
			}
		}
	}
}

func main() {
	runWatchdogCycle()
}
