package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	TelegramToken string
	AllowedChatID int64
)

const (
	AgentURL = "http://127.0.0.1:8888/agent"
)

type SecretsConfig struct {
	TelegramToken  string `json:"telegramToken"`
	TelegramChatID string `json:"telegramChatId"`
}

func initTelegramConfig() {
	TelegramToken = os.Getenv("TELEGRAM_TOKEN")
	if idStr := os.Getenv("TELEGRAM_CHAT_ID"); idStr != "" {
		AllowedChatID, _ = strconv.ParseInt(idStr, 10, 64)
	}

	if TelegramToken != "" && AllowedChatID != 0 {
		return
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
				if TelegramToken == "" && cfg.TelegramToken != "" {
					TelegramToken = cfg.TelegramToken
				}
				if AllowedChatID == 0 && cfg.TelegramChatID != "" {
					AllowedChatID, _ = strconv.ParseInt(cfg.TelegramChatID, 10, 64)
				}
			}
		}
	}
}

type BotInfo struct {
	Name       string
	Port       int
	Path       string
	RestartCmd []string
}

var botsMap = map[string]BotInfo{
	"huntera": {
		Name:       "⚔️ Huntera Idle",
		Port:       3000,
		Path:       "/api/status",
		RestartCmd: []string{"bash", "/home/ubuntu/huntera-go/start.sh"},
	},
	"tibidle": {
		Name:       "🛡️ Tibidle Multi-Bot",
		Port:       3001,
		Path:       "/api/status",
		RestartCmd: []string{"sudo", "systemctl", "restart", "tibidle.service"},
	},
	"sigma": {
		Name:       "⚡ Pokemon Sigma",
		Port:       3002,
		Path:       "/api/status",
		RestartCmd: []string{"sudo", "systemctl", "restart", "sigma-bot.service"},
	},
	"leveling": {
		Name:       "🏹 Leveling Idle 2D",
		Port:       3003,
		Path:       "/api/status",
		RestartCmd: []string{"sudo", "systemctl", "restart", "leveling-bot.service"},
	},
	"pokeidle": {
		Name:       "⚡ PokeIdle.io",
		Port:       3006,
		Path:       "/api/status",
		RestartCmd: []string{"sudo", "systemctl", "restart", "pokeidle.service"},
	},
	"pokeidleworld": {
		Name:       "🌍 Poke Idle World",
		Port:       3007,
		Path:       "/api/status",
		RestartCmd: []string{"sudo", "systemctl", "restart", "pokeidleworld.service"},
	},
	"kickminer": {
		Name:       "🟢 Kick Miner Go",
		Port:       3009,
		Path:       "/api/status",
		RestartCmd: []string{"sudo", "systemctl", "restart", "kick-miner.service"},
	},
	"twitchminer": {
		Name:       "🟣 Twitch Miner Go",
		Port:       3010,
		Path:       "/api/status",
		RestartCmd: []string{"sudo", "systemctl", "restart", "twitch-miner.service"},
	},
	"baiak": {
		Name:       "🏰 Baiak Idle Go",
		Port:       8090,
		Path:       "/api/fleet",
		RestartCmd: []string{"sudo", "systemctl", "restart", "baiak-fleet.service"},
	},
}

// Telegram Structures
type Update struct {
	UpdateID int      `json:"update_id"`
	Message  *Message `json:"message"`
}

type Message struct {
	MessageID int    `json:"message_id"`
	From      User   `json:"from"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
}

type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

type Chat struct {
	ID int64 `json:"id"`
}

type GetUpdatesResponse struct {
	OK     bool     `json:"ok"`
	Result []Update `json:"result"`
}

type SendMessagePayload struct {
	ChatID    int64  `json:"chat_id"`
	Text      string `json:"text"`
	ParseMode string `json:"parse_mode,omitempty"`
}

type SendActionPayload struct {
	ChatID int64  `json:"chat_id"`
	Action string `json:"action"`
}

type AgentRequest struct {
	Message string `json:"message"`
}

type AgentResponse struct {
	Response string `json:"response"`
	Error    string `json:"error"`
}

var (
	httpClient  = &http.Client{Timeout: 30 * time.Second}
	agentClient = &http.Client{Timeout: 120 * time.Second}
	quickClient = &http.Client{Timeout: 5 * time.Second}
)

func sendTelegramMessage(chatID int64, text string, parseMode string) error {
	// Chunk message if > 4000 characters
	for len(text) > 0 {
		chunk := text
		if len(chunk) > 4000 {
			cut := strings.LastIndex(chunk[:4000], "\n")
			if cut == -1 {
				cut = 4000
			}
			chunk = text[:cut]
			text = text[cut:]
		} else {
			text = ""
		}

		payload := SendMessagePayload{
			ChatID:    chatID,
			Text:      chunk,
			ParseMode: parseMode,
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}

		url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", TelegramToken)
		resp, err := httpClient.Post(url, "application/json", bytes.NewReader(data))
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK && parseMode != "" {
			// Fallback without parse_mode in case markdown was invalid
			payload.ParseMode = ""
			fallbackData, _ := json.Marshal(payload)
			fResp, fErr := httpClient.Post(url, "application/json", bytes.NewReader(fallbackData))
			if fErr == nil {
				fResp.Body.Close()
			}
		}
	}
	return nil
}

func sendChatAction(chatID int64, action string) {
	payload := SendActionPayload{
		ChatID: chatID,
		Action: action,
	}
	data, _ := json.Marshal(payload)
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendChatAction", TelegramToken)
	resp, err := quickClient.Post(url, "application/json", bytes.NewReader(data))
	if err == nil {
		resp.Body.Close()
	}
}

func getUpdates(offset int, timeoutSec int) ([]Update, error) {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?offset=%d&timeout=%d", TelegramToken, offset, timeoutSec)
	client := &http.Client{Timeout: time.Duration(timeoutSec+10) * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var res GetUpdatesResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	return res.Result, nil
}

func askGoAgent(prompt string) string {
	reqData, _ := json.Marshal(AgentRequest{Message: prompt})

	urls := []string{"http://127.0.0.1:8888/agent", "http://127.0.0.1:8080/agent"}
	if envURL := os.Getenv("AGENT_URL"); envURL != "" {
		urls = append([]string{envURL}, urls...)
	}

	var lastErr error
	for _, targetURL := range urls {
		resp, err := agentClient.Post(targetURL, "application/json", bytes.NewReader(reqData))
		if err != nil {
			lastErr = err
			continue
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Sprintf("[-] Erro ao ler resposta: %v", err)
		}

		var aResp AgentResponse
		if err := json.Unmarshal(bodyBytes, &aResp); err == nil {
			if aResp.Response != "" {
				return aResp.Response
			}
			if aResp.Error != "" {
				return fmt.Sprintf("[-] Erro da IA: %s", aResp.Error)
			}
		}
		return string(bodyBytes)
	}

	return fmt.Sprintf("[-] Erro ao conectar ao Agent Go: %v", lastErr)
}

func probePort(port int, path string) bool {
	url := fmt.Sprintf("http://127.0.0.1:%d%s", port, path)
	resp, err := quickClient.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func getMemInfo() string {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return ""
	}
	var total, avail int64
	lines := strings.Split(string(data), "\n")
	for _, l := range lines {
		parts := strings.Fields(l)
		if len(parts) >= 2 {
			if parts[0] == "MemTotal:" {
				total, _ = strconv.ParseInt(parts[1], 10, 64)
			} else if parts[0] == "MemAvailable:" {
				avail, _ = strconv.ParseInt(parts[1], 10, 64)
			}
		}
	}
	if total > 0 {
		used := (total - avail) / 1024
		totalMB := total / 1024
		return fmt.Sprintf("\n💾 *RAM:* %dMB usado / %dMB total", used, totalMB)
	}
	return ""
}

func handleFastStatus() string {
	var sb strings.Builder
	sb.WriteString("📊 *STATUS DOS 9 BOTS NA VPS (GO NATIVE)*\n\n")

	keys := []string{"huntera", "tibidle", "sigma", "leveling", "pokeidle", "pokeidleworld", "kickminer", "twitchminer", "baiak"}
	for _, k := range keys {
		bot := botsMap[k]
		online := probePort(bot.Port, bot.Path)
		statusIcon := "❌"
		statusText := "OFFLINE"
		if online {
			statusIcon = "✅"
			statusText = "ONLINE"
		}
		sb.WriteString(fmt.Sprintf("%s *%s* (:%d) → %s\n", statusIcon, bot.Name, bot.Port, statusText))
	}

	mem := getMemInfo()
	if mem != "" {
		sb.WriteString(mem)
	}

	return sb.String()
}

func handleMessage(msg *Message) {
	if msg == nil {
		return
	}

	// Security check
	if msg.Chat.ID != AllowedChatID {
		log.Printf("[seguranca] Mensagem bloqueada do ChatID: %d (%s)", msg.Chat.ID, msg.From.Username)
		_ = sendTelegramMessage(msg.Chat.ID, fmt.Sprintf("⛔ *Acesso Negado.*\nSeu Chat ID (`%d`) não possui autorização.", msg.Chat.ID), "Markdown")
		return
	}

	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}

	log.Printf("[telegram] Mensagem recebida: %q", text)

	switch {
	case text == "/start":
		welcome := "👋 *Olá, Comandante!*\n\n" +
			"Estou conectado ao seu **Agent IA em Go Puro** monitorando a VPS e vigiando os 9 bots 24/7 com Auto-Cura!\n\n" +
			"📌 *O que você pode fazer por aqui:*\n" +
			"• *Conversar livremente:* Digite qualquer pergunta ou instrução que a IA responderá!\n" +
			"• `/status` - Ver o status em tempo real dos 9 bots e memória da VPS\n" +
			"• `/restart <bot>` - Reiniciar um bot específico (ex: `/restart sigma`)\n" +
			"• `/logs <bot>` - Ver os logs e diagnóstico de erro de um bot com IA\n" +
			"• `/help` - Ver lista de comandos e bots disponíveis\n\n" +
			"_Operando 24/7 nativamente em Go!_ 🚀"
		_ = sendTelegramMessage(msg.Chat.ID, welcome, "Markdown")

	case text == "/help":
		help := "📖 *COMANDOS DISPONÍVEIS:*\n\n" +
			"• `/status` - Exibe a saúde dos 9 bots e RAM da VPS\n" +
			"• `/restart <bot>` - Reinicia um bot específico\n" +
			"• `/logs <bot>` - Analisa os últimos logs de um bot\n" +
			"  _Bots:_ `huntera`, `tibidle`, `sigma`, `leveling`, `pokeidle`, `pokeidleworld`, `kickminer`, `twitchminer`, `baiak`\n" +
			"• Ou envie *qualquer texto* para conversar com a IA com ferramentas completas de diagnóstico e auto-recuperação!"
		_ = sendTelegramMessage(msg.Chat.ID, help, "Markdown")

	case text == "/status":
		sendChatAction(msg.Chat.ID, "typing")
		statusReport := handleFastStatus()
		_ = sendTelegramMessage(msg.Chat.ID, statusReport, "Markdown")

	case strings.HasPrefix(text, "/logs"):
		target := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(text, "/logs")))
		if target == "" {
			_ = sendTelegramMessage(msg.Chat.ID, "⚠️ Especifique o bot para ver logs. Exemplo: `/logs sigma`", "Markdown")
			return
		}
		sendChatAction(msg.Chat.ID, "typing")
		prompt := fmt.Sprintf("Mostre os últimos logs do bot %s e forneça uma análise rápida do estado dele.", target)
		aiReply := askGoAgent(prompt)
		_ = sendTelegramMessage(msg.Chat.ID, aiReply, "Markdown")

	case strings.HasPrefix(text, "/restart"):
		target := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(text, "/restart")))
		bot, ok := botsMap[target]
		if !ok {
			keys := []string{"huntera", "tibidle", "sigma", "leveling", "pokeidle", "pokeidleworld", "kickminer", "twitchminer", "baiak"}
			_ = sendTelegramMessage(msg.Chat.ID, fmt.Sprintf("⚠️ Bot `%s` não reconhecido.\nUse um destes: `%s`", target, strings.Join(keys, "`, `")), "Markdown")
			return
		}

		sendChatAction(msg.Chat.ID, "typing")
		_ = sendTelegramMessage(msg.Chat.ID, fmt.Sprintf("🔄 Reiniciando *%s*...", bot.Name), "Markdown")

		cmd := exec.Command(bot.RestartCmd[0], bot.RestartCmd[1:]...)
		if err := cmd.Run(); err != nil {
			_ = sendTelegramMessage(msg.Chat.ID, fmt.Sprintf("❌ Falha ao reiniciar *%s*: %v", bot.Name, err), "Markdown")
		} else {
			_ = sendTelegramMessage(msg.Chat.ID, fmt.Sprintf("✅ Comando de reinício executado com sucesso para *%s*!", bot.Name), "Markdown")
		}

	default:
		// Conversa livre com o Agent Go
		sendChatAction(msg.Chat.ID, "typing")

		// Goroutine de typing indicator
		stopTyping := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(4 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					sendChatAction(msg.Chat.ID, "typing")
				case <-stopTyping:
					return
				}
			}
		}()

		aiReply := askGoAgent(text)
		close(stopTyping)
		wg.Wait()

		if aiReply == "" {
			aiReply = "(Sem resposta do agente)"
		}
		_ = sendTelegramMessage(msg.Chat.ID, aiReply, "Markdown")
	}
}

func main() {
	initTelegramConfig()
	if TelegramToken == "" || AllowedChatID == 0 {
		log.Fatal("[-] ERRO CRÍTICO: TELEGRAM_TOKEN ou TELEGRAM_CHAT_ID não definidos em env ou .secrets.json")
	}

	log.Println("=======================================================")
	log.Println("🚀 TELEGRAM AGENT GO (NATIVE LINUX DAEMON)")
	log.Printf("Chat ID Autorizado: %d\n", AllowedChatID)
	log.Printf("Conexão com Agent Go: %s\n", AgentURL)
	log.Println("=======================================================")

	offset := 0
	for {
		updates, err := getUpdates(offset, 25)
		if err != nil {
			log.Printf("[-] Erro getUpdates: %v. Aguardando 5s...", err)
			time.Sleep(5 * time.Second)
			continue
		}

		for _, u := range updates {
			offset = u.UpdateID + 1
			if u.Message != nil {
				handleMessage(u.Message)
			}
		}
	}
}
