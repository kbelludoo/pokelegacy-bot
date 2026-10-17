# ⚡ Game Bot Starter Kit & VPS Command Center (Go Native Engine)

Template padronizado de altíssima performance desenvolvido em **Go (Golang)** para criação rápida de bots autônomos para qualquer jogo de navegador / idle, além de um **Command Center Central** no GitHub Pages monitorando todos os bots em execução na VPS em tempo real.

🌐 **Dashboard Central (GitHub Pages):** [https://kbelludoo.github.io/bot-template-go/](https://kbelludoo.github.io/bot-template-go/)

---

## 🌟 Recursos Principais

- 📊 **VPS Command Center Integrado:** Acompanhe a saúde da VPS (CPU load, memória RAM, uptime) e todos os 9 bots rodando simultaneamente (Huntera, Tibidle, Pokemon Sigma, Leveling Idle 2D, Baiak Idle, PokeIdle.io, Poke Idle World, Kick Miner Go e Twitch Miner Go) em uma única interface Cyberpunk / Dark — agora 100% nativos em Go (zero Node/Bun).
- 🪶 **Ultra-leve:** Consumo típico entre **2 e 5 MB de RAM** por processo.
- 🛡️ **Supervisão Contínua:** Múltiplas contas em Goroutines isoladas com auto-reconexão e recuperação contra panic via `engine.RunWithSupervisor`.
- 🌐 **Web Dashboard Embutido:** Servidor HTTP local independente (`/api/status` e `/api/action`).
- 📺 **Console Ticker no Terminal:** Estatísticas de CPU/RAM, Kills, Ouro e Contas ativas limpas a cada 4 segundos.
- 🚀 **Pronto para VPS:** Zero navegadores na VPS. Script de compilação cruzada para Linux e template de serviço systemd incluídos.

---

## 📁 Estrutura do Projeto

```text
bot-template-go/
├── AI_INSTRUCTIONS.md          # 🤖 MANUAL COMPLETO PARA QUALQUER IA LER E GERAR NOVOS BOTS
├── accounts.example.json       # Modelo de configuração de contas
├── accounts.json               # Suas credenciais reais (ignorado pelo git)
├── main.go                     # Ponto de entrada universal
├── go.mod / go.sum             # Módulo Go nativo
│
├── docs/                       # 🌐 GITHUB PAGES (COMMAND CENTER AO VIVO)
│   ├── index.html              # Dashboard Cyberpunk agregador
│   └── status.json             # Telemetria em tempo real da VPS e bots
│
├── pkg/
│   ├── engine/                 # Motor: ciclo de vida, supervisor e gerenciador
│   ├── network/                # Conexões: WebSocket thread-safe e cliente HTTP
│   ├── auth/                   # Autenticação e CDP bridge para Cloudflare
│   ├── server/                 # Servidor HTTP e Dashboard embutido local
│   ├── stealth/                # Scrub de anonimização (nicks, e-mails, IPs)
│   ├── ghsync/                 # Wrappers da GitHub API via gh CLI
│   ├── vpsmetrics/             # Métricas nativas da VPS (/proc)
│   ├── scan/                   # Scanner de padrões de vazamento
│   └── game/                   # 🎮 AQUI ENTRA A LÓGICA DO NOVO JOGO
│
├── cmd/
│   ├── sync-vps-hub/           # Agregador de telemetria da VPS (hub 9 bots)
│   ├── sync-stealth/           # Telemetria individual anonimizada por repo
│   ├── sync-pages/             # Sync por bot (sigma/tibidle/leveling/generic)
│   ├── watchdog/               # Monitor de saúde + auto-cura + alertas Telegram
│   ├── huntera-web/            # Dashboard web leve com /api/status anônimo
│   ├── audit/                  # Auditoria de vazamentos (anon/verify/live/docs/...)
│   ├── deploy/                 # Aplicação de patches stealth na VPS
│   ├── fix-history/            # Scrub de token/IP no histórico git
│   ├── build/                  # Cross-compile para Linux/Windows
│   └── telegram-agent/         # Agente Telegram
│
└── scripts/
    ├── bot.service             # Template para systemd na VPS Ubuntu
    └── telegram-chat.service   # Template systemd do agente Telegram
```

---

## 🚀 Como Criar um Bot para um Novo Jogo

1. **Copie esta pasta:** Duplique a pasta `bot-template-go` e renomeie para o nome do jogo (ex: `meu-novo-bot`).
2. **Abra com a IA:** Peça para a IA ler o arquivo `AI_INSTRUCTIONS.md`. Ela já saberá exatamente a estrutura, os métodos e como codificar a pasta `pkg/game/`.
3. **Configure suas contas:** Copie `accounts.example.json` para `accounts.json` e adicione seus logins.
4. **Execute localmente:**
   ```bash
   go run .
   ```
5. **Acesse o Dashboard Local:** Abra `http://localhost:3001` (ou a porta configurada) no navegador.

---

## 📦 Como Compilar para VPS Linux

No Windows ou Linux (cmd Go nativo):
```cmd
go run ./cmd/build linux
```
Ou manualmente no terminal:
```cmd
cmd /c "set GOOS=linux&& set GOARCH=amd64&& go build -ldflags=\"-s -w\" -o bot-app-linux ."
```

Envie para a VPS:
```bash
scp bot-app-linux ubuntu@seu-servidor-ip:/home/ubuntu/meu-bot/
```

---

## 🔄 Sincronização 24/7 com GitHub Pages

Para manter o status da VPS e de todos os bots atualizado automaticamente no GitHub Pages a cada 2 minutos via cron:

```bash
# Na VPS (binário Go nativo):
go build -o sync-vps-hub ./cmd/sync-vps-hub
crontab -e
# Adicione a linha:
*/2 * * * * /home/ubuntu/bot/sync-vps-hub --push >/dev/null 2>&1
```

### Outros cmds úteis (cron / manual)

```bash
# Telemetria individual anonimizada (sigma/tibidle/leveling/etc)
go build -o sync-stealth ./cmd/sync-stealth

# Watchdog de saúde dos 9 bots + auto-cura + alertas Telegram
go build -o watchdog ./cmd/watchdog
* * * * * /home/ubuntu/bot/watchdog >/dev/null 2>&1

# Auditoria de vazamentos
go build -o audit ./cmd/audit
./audit anon -deep
./audit verify
./audit live
```
