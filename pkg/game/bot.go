package game

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"sync"
	"time"

	"bot-template-go/pkg/engine"
)

// MaxStaminaMs limite máximo de stamina diária no PokeLegacy (12 horas = 43.200.000 ms)
const MaxStaminaMs int64 = 43200000

// Bot implementa a interface engine.GameBot para o PokeLegacy Online
type Bot struct {
	config  AccountConfig
	mu      sync.RWMutex
	status  engine.AccountStatus
	client  *LegacyClient
	socket  *SocketClient
	cb      *engine.CircuitBreaker

	// Estado do jogo sincronizado com o servidor (Pilar 3: Preservação de Schema)
	user            User
	rawSave         map[string]any
	saveVersion     int
	clientVersion   int
	saveOwnedFields []string

	// Lógica de jogo e combate
	trainerLevel  int
	trainerExp    int64
	trainerName   string
	money         int64
	totalKills    int
	totalCatches  int
	team          []Pokemon
	activeMon     Pokemon
	currentSpot   Spot
	currentRoute  string
	isHunting     bool
	inCity        bool
	isResting     bool
	staminaMs     int64
	pokedexCaught map[int]bool

	// Sessão de combate oficial em tempo real via WebSocket
	serverSessionID string
	serverRoster    []map[string]any
	lastRosterReq   time.Time

	// Métricas de sessão
	sessionStart    time.Time
	sessionKills    int
	sessionExp      int64
	lastSaveAt      time.Time
	lastDailyCheck  time.Time
	lastMaintenance time.Time
	lastDonationAt  time.Time
	lastSellCheck   time.Time

	// Gestão de Party (Grupo Cooperativo)
	partyID       string
	partyLeaderID string
	partyMembers  []string
	isInParty     bool

	ticketChan    chan string
	turnstileChan chan string
}

// NewBot inicializa uma nova instância de bot para PokeLegacy
func NewBot(cfg AccountConfig) *Bot {
	st := engine.NewStatus(cfg.ID, cfg.Name)
	st.Vocation = "Treinador"
	st.CurrentArea = cfg.Settings.HuntTarget

	cbCfg := engine.DefaultCircuitBreakerConfig()
	cb := engine.NewCircuitBreaker(cfg.ID, cbCfg)

	deviceID := cfg.Credentials.DeviceID
	client := NewLegacyClient(DefaultBaseURL, cfg.BindIP, cfg.Proxy, deviceID, 20*time.Second)

	b := &Bot{
		config:        cfg,
		status:        st,
		client:        client,
		cb:            cb,
		rawSave:       make(map[string]any),
		pokedexCaught: make(map[int]bool),
		ticketChan:    make(chan string, 1),
		turnstileChan: make(chan string, 1),
		sessionStart:  time.Now(),
		isHunting:     cfg.Settings.AutoHunt,
		saveVersion:   1,
		clientVersion: 1,
	}

	cbCfg.OnTrip = func(reason string) {
		b.mu.Lock()
		b.isHunting = false
		b.inCity = true
		b.status.Status = "DISJUNTOR ABERTO (Anti-Ban Ativo)"
		b.status.Logs = append(b.status.Logs, "🚨 Combate suspenso por proteção anti-ban: "+reason)
		b.mu.Unlock()
	}

	cbCfg.OnReset = func() {
		b.mu.Lock()
		b.isHunting = b.config.Settings.AutoHunt
		b.status.Status = "hunting"
		b.status.Logs = append(b.status.Logs, "✅ Disjuntor restaurado. Retomando caça com segurança.")
		b.mu.Unlock()
	}

	return b
}

func (b *Bot) ID() string {
	return b.config.ID
}

func (b *Bot) Name() string {
	return b.config.Name
}

// Init autentica a conta e carrega o save canônico inicial
func (b *Bot) Init(ctx context.Context) error {
	b.updateStatus(func(s *engine.AccountStatus) {
		s.Status = "Autenticando..."
	})

	// 1. Autenticação via Token ou Login/Senha
	if b.config.Credentials.Token != "" {
		b.client.SetToken(b.config.Credentials.Token)
		b.addLog("Utilizando Bearer Token configurado no accounts.json")
	} else {
		loginTarget := b.config.Credentials.Email
		if loginTarget == "" {
			loginTarget = b.config.Credentials.Username
		}
		if loginTarget == "" || b.config.Credentials.Password == "" {
			return fmt.Errorf("[%s] Credenciais ausentes (forneça email/senha ou token)", b.ID())
		}

		b.addLog(fmt.Sprintf("Autenticando em /api/login para %s...", loginTarget))
		res, err := b.client.Login(ctx, loginTarget, b.config.Credentials.Password)
		if err != nil {
			b.cb.RecordFailure(err.Error())
			return fmt.Errorf("falha no login: %w", err)
		}
		b.user = res.User
		b.saveVersion = res.SaveVersion
		b.clientVersion = res.ClientVersion
		b.saveOwnedFields = res.SaveOwnedFields
		b.addLog("Autenticado com sucesso via /api/login.")
	}

	// 2. Carrega o estado oficial do jogo via GET /api/me
	me, err := b.client.FetchMe(ctx)
	if err != nil {
		b.cb.RecordFailure(err.Error())
		return fmt.Errorf("falha ao obter /api/me: %w", err)
	}

	b.mu.Lock()
	b.user = me.User
	b.rawSave = me.Save
	if me.SaveVersion > 0 {
		b.saveVersion = me.SaveVersion
	}
	if me.ClientVersion > 0 {
		b.clientVersion = me.ClientVersion
	}
	if len(me.SaveOwnedFields) > 0 {
		b.saveOwnedFields = me.SaveOwnedFields
	}

	b.extractGameStateLocked()
	b.selectOptimalRouteLocked()
	b.mu.Unlock()

	b.cb.RecordSuccess()

	// 3. Manutenção inicial (Recompensas, Jornada, Missões, Consumíveis)
	go func() {
		time.Sleep(5 * time.Second)
		b.runMaintenance(ctx)
	}()

	b.addLog(fmt.Sprintf("Pronto. Treinador Lv. %d | Time: %d Pokémon | Rota: %s",
		b.trainerLevel, len(b.team), b.currentSpot.Name))

	// 4. Inicia WebSocket Socket.IO (desconecta aba ativa do navegador e mantém presença oficial)
	b.socket = NewSocketClient(
		DefaultBaseURL,
		b.client.Token(),
		b.config.Credentials.DeviceID,
		b.config.BindIP,
		b.config.Proxy,
		b.ID(),
		func(reason string) {
			b.cb.RecordFailure("Sessão tomada por outro cliente (outraAba)")
		},
	)

	// Consolidação de caça oficial retornada via Socket.IO
	b.socket.On("hunt:resumeResponse", func(payload any) {
		if pMap, ok := payload.(map[string]any); ok {
			off, _ := pMap["official"].(map[string]any)
			v, _ := pMap["version"].(int)
			b.mu.Lock()
			defer b.mu.Unlock()
			b.mergeOfficialLocked(off, v)
			b.addLogLocked(fmt.Sprintf("🔄 Sincronizado com servidor: Nível %d (%d EXP) | Ouro: %d",
				b.trainerLevel, b.trainerExp, b.money))
		}
	})

	// Registra ouvinte para atualizações de estado da Party
	b.socket.On("party:state", func(payload any) {
		b.mu.Lock()
		defer b.mu.Unlock()
		if payload == nil {
			b.isInParty = false
			b.partyID = ""
			b.partyLeaderID = ""
			b.partyMembers = nil
			return
		}
		if pMap, ok := payload.(map[string]any); ok {
			b.isInParty = true
			b.partyID = fmt.Sprintf("%v", pMap["partyId"])
			b.partyLeaderID = fmt.Sprintf("%v", pMap["leaderId"])
			if members, ok := pMap["members"].([]any); ok {
				b.partyMembers = make([]string, 0, len(members))
				for _, m := range members {
					if mm, ok := m.(map[string]any); ok {
						b.partyMembers = append(b.partyMembers, fmt.Sprintf("%v", mm["userId"]))
					}
				}
			}
			b.status.Logs = append(b.status.Logs, fmt.Sprintf("👥 Party ativa (%d membros, Líder: %s)", len(b.partyMembers), b.partyLeaderID))
		}
	})

	// Auto-accept de proposta de caçada em grupo enviada pelo líder
	b.socket.On("party:huntProposal", func(payload any) {
		b.mu.Lock()
		teamUids := make([]string, 0, len(b.team))
		for _, m := range b.team {
			if m.UID != "" && m.UID != "<nil>" {
				teamUids = append(teamUids, m.UID)
			}
		}
		b.mu.Unlock()

		b.addLog("👥 Proposta de caçada em grupo recebida. Aceitando...")
		_ = b.socket.Emit("party:huntAnswer", map[string]any{
			"accept":   true,
			"teamUids": teamUids,
		})
	})

	// Caçada em party iniciada com sucesso
	b.socket.On("party:huntStart", func(payload any) {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.status.Logs = append(b.status.Logs, "⚔️ CAÇADA EM PARTY INICIADA! Compartilhando EXP e bônus de grupo.")
	})

	// Abate compartilhado em party (recebimento de EXP bônus de grupo)
	b.socket.On("party:killShare", func(payload any) {
		if pMap, ok := payload.(map[string]any); ok {
			share := int64(toFloat(pMap["share"]))
			fromName := fmt.Sprintf("%v", pMap["fromName"])
			if share > 0 {
				b.mu.Lock()
				b.sessionExp += share
				b.trainerExp += share
				b.totalKills++
				b.status.Logs = append(b.status.Logs, fmt.Sprintf("💥 Abate compartilhado por %s! +%d EXP", fromName, share))
				newLevel := ExpToLevel(b.trainerExp)
				if newLevel > b.trainerLevel {
					b.trainerLevel = newLevel
					b.status.Logs = append(b.status.Logs, fmt.Sprintf("🌟 SUBIU DE NÍVEL! Nível de Treinador: %d", b.trainerLevel))
					b.selectOptimalRouteLocked()
				}
				b.mu.Unlock()
			}
		}
	})

	// Aviso de nível incompatível / barrado
	b.socket.On("party:huntBarred", func(payload any) {
		reason := "Você ficou de fora da hunt da party (diferença de nível > 25)."
		if pMap, ok := payload.(map[string]any); ok {
			if r, ok := pMap["reason"].(string); ok && r != "" {
				reason = r
			}
		}
		b.addLog("⚠️ " + reason)
	})

	b.socket.On("party:levelNotice", func(payload any) {
		reason := "Níveis incompatíveis para caçar em party (máx 25 níveis de diferença)."
		if pMap, ok := payload.(map[string]any); ok {
			if r, ok := pMap["reason"].(string); ok && r != "" {
				reason = r
			}
		}
		b.addLog("⚠️ " + reason)
	})

	b.socket.StartLoop(ctx)

	return nil
}

// Run executa o loop autônomo do bot de jogo
func (b *Bot) Run(ctx context.Context) error {
	b.updateStatus(func(s *engine.AccountStatus) {
		s.Status = "hunting"
	})

	saveInterval := time.Duration(b.config.Settings.SaveIntervalSec) * time.Second
	if saveInterval < 15*time.Second {
		saveInterval = 30 * time.Second
	}

	b.lastSaveAt = time.Now()
	b.lastSellCheck = time.Now()

	// Loop contínuo com jitter humanoide (Pilar de Discrição e Anti-Ban)
	for {
		select {
		case <-ctx.Done():
			if b.socket != nil {
				b.socket.Close()
			}
			// Desligamento limpo: salva estado pendente antes de sair
			b.flushSave(context.Background())
			return ctx.Err()
		default:
		}

		// Intervalo randômico entre encontros com Throttling Coletivo de Party (Pilar 2)
		sleepMs := 3200 + rand.Intn(1300)
		b.mu.RLock()
		inP := b.isInParty
		mCount := len(b.partyMembers)
		b.mu.RUnlock()

		if inP && mCount > 1 {
			bonusExp := 15.0
			switch mCount {
			case 2:
				bonusExp = 15.0
			case 3:
				bonusExp = 25.0
			case 4:
				bonusExp = 35.0
			case 5:
				bonusExp = 40.0
			}
			indivRate := engine.CalculatePartyMaxKillRate(18.0, mCount, bonusExp)
			if indivRate > 0 {
				sleepMs = int((60.0 / indivRate) * 1000)
				sleepMs += rand.Intn(800) - 400
			}
		}
		select {
		case <-ctx.Done():
			if b.socket != nil {
				b.socket.Close()
			}
			return ctx.Err()
		case <-time.After(time.Duration(sleepMs) * time.Millisecond):
		}

		// Verifica permissão do Circuit Breaker
		if !b.cb.AllowAction() {
			rem := b.cb.RemainingCooldown()
			b.updateStatus(func(s *engine.AccountStatus) {
				s.Status = fmt.Sprintf("COOLDOWN ANTI-BAN (%s)", rem.Round(time.Second))
			})
			time.Sleep(5 * time.Second)
			continue
		}

		// Se estiver no modo Half-Open, recarrega o estado do servidor com cautela
		if b.cb.State() == engine.StateHalfOpen {
			b.addLog("Modo HALF-OPEN: testando integridade com GET /api/me...")
			me, err := b.client.FetchMe(ctx)
			if err != nil {
				if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "não autenticado") {
					_ = b.relogin(ctx)
					me, err = b.client.FetchMe(ctx)
				}
			}
			if err != nil {
				b.cb.RecordFailure("Falha no teste canário /api/me: " + err.Error())
				continue
			}
			b.mu.Lock()
			b.rawSave = me.Save
			b.saveVersion = me.SaveVersion
			b.clientVersion = me.ClientVersion
			if len(me.SaveOwnedFields) > 0 {
				b.saveOwnedFields = me.SaveOwnedFields
			}
			b.extractGameStateLocked()
			b.mu.Unlock()
			b.cb.RecordSuccess()
			b.addLog("Teste canário bem-sucedido. Retomando caça normal.")
		}

		// Executa combate oficial no servidor via WebSocket
		if b.socket != nil && b.socket.IsConnected() {
			b.performServerHuntStep(ctx)
		}

		// Executa passo de simulação do jogo com tempo decorrido para consumo de stamina
		b.performGameStep(ctx, sleepMs)

		// Verifica se é hora de salvar o progresso no servidor
		if time.Since(b.lastSaveAt) >= saveInterval {
			b.flushSave(ctx)
		}

		// Manutenção Automática Periódica (Missões, Jornada, Consumíveis, Loja, Sell/Stash, Oficina, Buffs)
		if time.Since(b.lastMaintenance) >= 3*time.Minute {
			b.lastMaintenance = time.Now()
			go b.runMaintenance(ctx)
		}
	}
}

// performServerHuntStep executa o combate oficial diretamente nos servidores do PokeLegacy via WebSocket
func (b *Bot) performServerHuntStep(ctx context.Context) {
	if b.socket == nil || !b.socket.IsConnected() {
		return
	}

	b.mu.Lock()
	if !b.isHunting || b.inCity || b.isResting || b.staminaMs <= 0 {
		b.mu.Unlock()
		return
	}

	routeID := b.currentSpot.ID
	if routeID == "" {
		routeID = "kanto_campos_de_pallet"
	}

	// 1. Se não tiver sessão ou roster ativo, solicita novo hunt:roster
	if b.serverSessionID == "" || len(b.serverRoster) == 0 {
		if time.Since(b.lastRosterReq) < 3*time.Second {
			b.mu.Unlock()
			return
		}
		b.lastRosterReq = time.Now()

		teamUids := make([]string, 0, len(b.team))
		for _, m := range b.team {
			if m.UID != "" {
				teamUids = append(teamUids, m.UID)
			}
		}

		rosterPayload := map[string]any{
			"routeId":     routeID,
			"count":       10,
			"teamUids":    teamUids,
			"controlMode": "auto",
			"depotDelta":  1,
		}
		b.mu.Unlock()

		_ = b.socket.EmitAck("hunt:roster", rosterPayload, func(resp []any) {
			if len(resp) == 0 {
				return
			}
			rMap, ok := resp[0].(map[string]any)
			if !ok {
				return
			}

			if okVal, _ := rMap["ok"].(bool); okVal {
				b.mu.Lock()
				defer b.mu.Unlock()
				b.serverSessionID, _ = rMap["sessionId"].(string)
				if rosterRaw, ok := rMap["roster"].([]any); ok {
					b.serverRoster = make([]map[string]any, 0, len(rosterRaw))
					for _, raw := range rosterRaw {
						if m, ok := raw.(map[string]any); ok {
							b.serverRoster = append(b.serverRoster, m)
						}
					}
				}
				if off, ok := rMap["official"].(map[string]any); ok {
					b.mergeOfficialLocked(off, int(toFloat(rMap["version"])))
				}
			} else if code, _ := rMap["code"].(string); code == "AFK_SETTLEMENT_PENDING" {
				_ = b.socket.ResumeHunt()
			}
		})
		return
	}

	// 2. Se temos monstro no roster, executa hit com golpe válido
	firstWild := b.serverRoster[0]
	wildID := firstWild["wildId"]
	sessID := b.serverSessionID

	moveID := "tackle"
	if len(b.activeMon.Moves) > 0 {
		moveID = b.activeMon.Moves[0]
	}

	var ballID any = nil
	spID := int(toFloat(firstWild["speciesId"]))
	if b.config.Settings.AutoCatch || (b.config.Settings.FocusPokedex && !b.pokedexCaught[spID]) {
		ballID = "poke_ball"
		if b.config.Settings.PreferredBall != "" {
			ballID = b.config.Settings.PreferredBall
		}
	}

	activeMonUID := b.activeMon.UID
	castID := fmt.Sprintf("cast:%d", time.Now().UnixNano())

	hitsPayload := map[string]any{
		"sessionId":     sessID,
		"castId":        castID,
		"moveId":        moveID,
		"activeMonUid":  activeMonUID,
		"primaryWildId": wildID,
		"hits": []map[string]any{
			{
				"wildId": wildID,
				"ballId": ballID,
			},
		},
	}
	b.mu.Unlock()

	_ = b.socket.EmitAck("hunt:rosterHits", hitsPayload, func(resp []any) {
		if len(resp) == 0 {
			return
		}
		rMap, ok := resp[0].(map[string]any)
		if !ok {
			return
		}

		b.mu.Lock()
		defer b.mu.Unlock()

		if okVal, _ := rMap["ok"].(bool); okVal {
			if results, ok := rMap["results"].([]any); ok && len(results) > 0 {
				if res0, ok := results[0].(map[string]any); ok {
					if res0["killed"] == true {
						if len(b.serverRoster) > 0 {
							b.serverRoster = b.serverRoster[1:]
						}
						if respawn, ok := res0["respawn"].(map[string]any); ok {
							b.serverRoster = append(b.serverRoster, respawn)
						}
					}
					if off, ok := res0["official"].(map[string]any); ok {
						v := int(toFloat(res0["version"]))
						b.mergeOfficialLocked(off, v)
					}
				}
			}
		} else {
			code, _ := rMap["code"].(string)
			if code == "AFK_SETTLEMENT_PENDING" {
				_ = b.socket.ResumeHunt()
				b.serverSessionID = ""
			} else if code == "NO_SESSION" || code == "STALE_SESSION" || code == "ROSTER_TIMEOUT" || code == "MOVE_NOT_ALLOWED" {
				b.serverSessionID = ""
			}
		}
	})
}

// performGameStep simula uma rodada de caça, cálculo de dano e abates
func (b *Bot) performGameStep(ctx context.Context, elapsedMs int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.isHunting {
		return
	}

	// 0. Gestão Inteligente de Stamina & Auto-Recuperação (Pilar Anti-Ban & Auto-Sustentabilidade)
	todayStr := time.Now().Format("2006-01-02")
	if refillMap, ok := b.rawSave["staminaRefill"].(map[string]any); ok {
		lastDate, _ := refillMap["dailyDate"].(string)
		uses := int(toFloat(refillMap["dailyUses"]))
		if lastDate != todayStr || uses == 0 {
			if b.staminaMs < MaxStaminaMs/2 || b.isResting {
				b.staminaMs = MaxStaminaMs
				refillMap["dailyUses"] = 1
				refillMap["dailyDate"] = todayStr
				b.isResting = false
				b.inCity = false
				b.addLogLocked("⚡ STAMINA: Refill diário gratuito ativado automaticamente (+12h)!")
			}
		}
	}

	if b.staminaMs <= 0 || b.isResting {
		b.inCity = true
		b.isResting = true
		b.status.Status = "DESCANSO (Cidade)"

		// Regenera stamina enquanto descansa na cidade (1s de stamina a cada 3s de descanso real)
		b.staminaMs += int64(elapsedMs) / 3
		if b.staminaMs >= MaxStaminaMs {
			b.staminaMs = MaxStaminaMs
			b.isResting = false
			b.inCity = false
			b.addLogLocked("⚡ STAMINA: Recuperação total concluída (12h). Retomando caça.")
		} else if b.staminaMs >= 7200000 { // Margem de segurança de pelo menos 2 horas
			b.isResting = false
			b.inCity = false
			b.addLogLocked("⚡ STAMINA: Margem de segurança restabelecida (2h+). Retomando caça.")
		}

		b.status.StaminaMs = b.staminaMs
		b.status.MaxStaminaMs = MaxStaminaMs
		b.status.StaminaPct = int((float64(b.staminaMs) / float64(MaxStaminaMs)) * 100)
		totalSec := b.staminaMs / 1000
		hours := totalSec / 3600
		mins := (totalSec % 3600) / 60
		b.status.StaminaFormatted = fmt.Sprintf("%dh %02dm", hours, mins)
		return
	} else {
		b.inCity = false
		b.status.Status = "hunting"
		b.staminaMs -= int64(elapsedMs)
		if b.staminaMs < 0 {
			b.staminaMs = 0
		}
	}

	// 1. Verificação de Saúde / AutoHeal
	minHp := b.config.Settings.MinHpPercent
	if minHp <= 0 {
		minHp = 25
	}
	if b.activeMon.MaxHP > 0 && (b.activeMon.HP*100)/b.activeMon.MaxHP <= minHp {
		if b.config.Settings.AutoHeal {
			b.activeMon.HP = b.activeMon.MaxHP
			for i := range b.team {
				if b.team[i].MaxHP > 0 {
					b.team[i].HP = b.team[i].MaxHP
				}
			}
			b.status.Logs = append(b.status.Logs, "❤️ Time descansou no Pokécenter. HP 100% restaurado.")
			if len(b.status.Logs) > 10 {
				b.status.Logs = b.status.Logs[1:]
			}
		}
	}

	// 2. Escolha de alvo na rota atual
	var wildID int
	if len(b.currentSpot.Species) > 0 {
		wildID = b.currentSpot.Species[rand.Intn(len(b.currentSpot.Species))]
	} else {
		wildID = 19 // Rattata fallback
	}

	spInfo, _ := GetSpecies(wildID)
	wildName := spInfo.Name
	if wildName == "" {
		wildName = fmt.Sprintf("Pokémon #%d", wildID)
	}
	wildType := b.currentSpot.TypeGroup
	if len(spInfo.Types) > 0 {
		wildType = spInfo.Types[0]
	}

	// 3. EXPLORAR FRAQUEZAS: Escolhe dinamicamente o melhor Pokémon do time para enfrentar o alvo
	mult := 1.0
	if b.config.Settings.ExploitWeakness && len(b.team) > 0 {
		bestMonIdx := 0
		bestMult := 0.0

		for idx, mon := range b.team {
			monSpecies, _ := GetSpecies(mon.SpeciesID)
			monTypes := monSpecies.Types
			if len(monTypes) == 0 {
				monTypes = []string{"normal"}
			}

			// Multiplicador do nosso ataque contra o tipo do selvagem
			for _, attackerT := range monTypes {
				m := TypeEffectiveness(attackerT, wildType)
				if m > bestMult {
					bestMult = m
					bestMonIdx = idx
				}
			}
		}

		if bestMult > 0 {
			mult = bestMult
			b.activeMon = b.team[bestMonIdx]
		}
	} else {
		monType := "normal"
		if len(b.activeMon.Moves) > 0 {
			monType = b.activeMon.Moves[0]
		}
		mult = TypeEffectiveness(monType, wildType)
	}

	if mult >= 2.0 && rand.Float64() < 0.20 {
		b.status.Logs = append(b.status.Logs, fmt.Sprintf("💥 Super Efetivo (%.1fx)! %s explorou a fraqueza de %s (%s).", mult, b.activeMon.Name, wildName, wildType))
	}

	expGain := int64(float64(spInfo.BaseExp) * mult * (1.0 + float64(b.trainerLevel)*0.08))
	if expGain <= 0 {
		expGain = 45
	}
	if b.staminaMs <= 0 {
		expGain = 0 // Stamina esgotada: 0% de ganho de EXP
	}

	goldGain := int64(12 + rand.Intn(25) + (b.trainerLevel * 2))

	// 4. Aplica ganhos no personagem
	b.totalKills++
	b.sessionKills++
	b.sessionExp += expGain
	b.trainerExp += expGain
	b.money += goldGain
	b.activeMon.Exp += expGain

	// Dano recebido leve
	dmg := rand.Intn(10) + 2
	if b.activeMon.HP > dmg {
		b.activeMon.HP -= dmg
	} else {
		b.activeMon.HP = b.activeMon.MaxHP / 2
	}

	// 5. FOCO NO BESTIÁRIO: Prioriza captura se o Pokémon ainda não foi registrado
	isNewSpecies := !b.pokedexCaught[wildID]
	catchChance := 0.22
	if isNewSpecies && (b.config.Settings.FocusPokedex || b.config.Settings.AutoCatch) {
		catchChance = 0.65 // Chance aumentada focada no Bestiário
		if rand.Float64() < 0.35 {
			b.status.Logs = append(b.status.Logs, fmt.Sprintf("📖 Alvo do Bestiário avistado: %s (#%d)! Jogando Pokébola...", wildName, wildID))
		}
	}

	if (b.config.Settings.AutoCatch || (isNewSpecies && b.config.Settings.FocusPokedex)) && rand.Float64() < catchChance {
		b.totalCatches++
		b.pokedexCaught[wildID] = true

		// Registra no rawSave["pokedex"]["caught"]
		if pDex, ok := b.rawSave["pokedex"].(map[string]any); ok {
			if caughtMap, ok := pDex["caught"].(map[string]any); ok {
				caughtMap[fmt.Sprintf("%d", wildID)] = true
			}
		}

		if isNewSpecies {
			b.status.Logs = append(b.status.Logs, fmt.Sprintf("✨ REGISTRADO NO BESTIÁRIO: %s (#%d)! (+1 Entrada)", wildName, wildID))
			// Se completou a rota atual no bestiário, reavalia a rota
			if b.config.Settings.FocusPokedex {
				b.selectOptimalRouteLocked()
			}
		} else {
			b.status.Logs = append(b.status.Logs, fmt.Sprintf("🎯 Capturado: %s selvagem!", wildName))
		}
	}

	// Atualiza nível do treinador e recálculo dinâmico de rota usando curva canônica oficial (Pilar 1)
	newLevel := ExpToLevel(b.trainerExp)
	if newLevel > b.trainerLevel {
		b.trainerLevel = newLevel
		b.status.Logs = append(b.status.Logs, fmt.Sprintf("🌟 SUBIU DE NÍVEL! Nível de Treinador: %d", b.trainerLevel))
		b.selectOptimalRouteLocked()
	}

	// 5. Atualiza AccountStatus
	duration := int(time.Since(b.sessionStart).Seconds())
	if duration <= 0 {
		duration = 1
	}
	expPerHour := (float64(b.sessionExp) / float64(duration)) * 3600.0

	b.status.HP = b.activeMon.HP
	b.status.MaxHP = b.activeMon.MaxHP
	b.status.Mana = 100
	b.status.MaxMana = 100
	b.status.Gold = b.money
	b.status.Level = b.trainerLevel
	b.status.Kills = b.totalKills
	b.status.ExpPerHour = expPerHour
	b.status.CurrentArea = b.currentSpot.Name
	b.status.DurationSec = duration
	b.status.UpdatedAt = time.Now().UnixMilli()

	b.status.StaminaMs = b.staminaMs
	b.status.MaxStaminaMs = MaxStaminaMs
	b.status.StaminaPct = int((float64(b.staminaMs) / float64(MaxStaminaMs)) * 100)
	totalSec := b.staminaMs / 1000
	hours := totalSec / 3600
	mins := (totalSec % 3600) / 60
	b.status.StaminaFormatted = fmt.Sprintf("%dh %02dm", hours, mins)

	if len(b.status.Logs) > 10 {
		b.status.Logs = b.status.Logs[len(b.status.Logs)-10:]
	}
}

// relogin reautentica a conta caso o Bearer token expire (ex: após 12h de sessão)
func (b *Bot) relogin(ctx context.Context) error {
	loginTarget := b.config.Credentials.Email
	if loginTarget == "" {
		loginTarget = b.config.Credentials.Username
	}
	if loginTarget == "" || b.config.Credentials.Password == "" {
		return fmt.Errorf("credenciais ausentes para relogin")
	}

	res, err := b.client.Login(ctx, loginTarget, b.config.Credentials.Password)
	if err != nil {
		return fmt.Errorf("falha ao renovar login: %w", err)
	}

	b.mu.Lock()
	b.client.SetToken(res.Token)
	b.config.Credentials.Token = res.Token
	if b.socket != nil {
		b.socket.SetToken(res.Token)
	}
	b.mu.Unlock()

	b.addLog("🔑 Sessão renovada com sucesso via /api/login (sessão de 12h estendida).")
	return nil
}

// flushSave realiza o envio do save com o envelope estrito e Dynamic Overlay
func (b *Bot) flushSave(ctx context.Context) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.user.GetID() == "" {
		return
	}

	// Atualiza o RawSave existente preservando 100% das chaves desconhecidas do servidor
	b.rawSave["money"] = b.money
	b.rawSave["trainerLevel"] = b.trainerLevel
	b.rawSave["trainerExp"] = b.trainerExp
	b.rawSave["staminaMs"] = b.staminaMs
	b.rawSave["inCity"] = b.inCity
	b.rawSave["currentRoute"] = b.currentSpot.ID
	b.rawSave["lastSave"] = time.Now().UnixMilli()

	// Atualiza status do save
	statsMap, ok := b.rawSave["stats"].(map[string]any)
	if !ok || statsMap == nil {
		statsMap = make(map[string]any)
	}
	statsMap["kills"] = b.totalKills
	statsMap["catches"] = b.totalCatches
	b.rawSave["stats"] = statsMap

	// Atualiza HP dos monstrinhos no time
	if teamSlice, ok := b.rawSave["team"].([]any); ok && len(teamSlice) > 0 {
		if firstMon, ok := teamSlice[0].(map[string]any); ok {
			firstMon["hp"] = b.activeMon.HP
		}
	}

	// Prepara Envelope conforme esperado por /api/save
	envelope := SaveEnvelope{
		Save:              b.rawSave,
		AccountID:         b.user.ID,
		BaseVersion:       b.saveVersion,
		BaseClientVersion: b.clientVersion,
		Omitted:           b.saveOwnedFields,
	}

	b.lastSaveAt = time.Now()

	// Envio em background com supervisão
	go func(env SaveEnvelope) {
		res, err := b.client.PushSave(ctx, env)
		if err != nil {
			// Se for 409 SAVE_CONTRACT_STALE, reconcilia contrato sem disparar o Circuit Breaker
			if strings.Contains(err.Error(), "SAVE_CONTRACT_STALE") {
				log.Printf("[%s] 🔄 Detectado SAVE_CONTRACT_STALE no erro. Reconciliando com /api/me...", b.ID())
				me, errMe := b.client.FetchMe(ctx)
				if errMe == nil && me != nil {
					b.mu.Lock()
					b.rawSave = me.Save
					b.saveVersion = me.SaveVersion
					b.clientVersion = me.ClientVersion
					if len(me.SaveOwnedFields) > 0 {
						b.saveOwnedFields = me.SaveOwnedFields
					}
					b.extractGameStateLocked()
					env.Save = b.rawSave
					env.BaseVersion = me.SaveVersion
					env.BaseClientVersion = me.ClientVersion
					env.Omitted = b.saveOwnedFields
					b.mu.Unlock()

					res2, err2 := b.client.PushSave(ctx, env)
					if err2 == nil && res2 != nil && res2.Code != "SAVE_CONTRACT_STALE" {
						b.mu.Lock()
						if res2.Version > 0 {
							b.saveVersion = res2.Version
						}
						if res2.ClientVersion > 0 {
							b.clientVersion = res2.ClientVersion
						}
						b.mu.Unlock()
						b.cb.RecordSuccess()
						log.Printf("[%s] ✅ Save re-enviado com sucesso após reconciliação de contrato!", b.ID())
						return
					}
				}
				return
			}

			// Se o erro for 401 ("não autenticado" ou token expirado após 12h)
			if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "não autenticado") {
				log.Printf("[%s] 🔑 Token expirado (12h). Renovando sessão...", b.ID())
				if rErr := b.relogin(ctx); rErr == nil {
					res2, err2 := b.client.PushSave(ctx, env)
					if err2 == nil {
						b.mu.Lock()
						if res2.Version > 0 {
							b.saveVersion = res2.Version
						}
						if res2.ClientVersion > 0 {
							b.clientVersion = res2.ClientVersion
						}
						if res2.Save != nil && len(res2.Save) > 0 {
							b.rawSave = res2.Save
							b.extractGameStateLocked()
						}
						b.mu.Unlock()
						b.cb.RecordSuccess()
						return
					}
				}
			}

			log.Printf("[%s] Erro ao salvar progresso em /api/save: %v", b.ID(), err)
			b.cb.RecordFailure("Erro ao salvar progresso: " + err.Error())
			return
		}

		if res != nil && res.Code == "SAVE_CONTRACT_STALE" {
			log.Printf("[%s] 🔄 Resposta SAVE_CONTRACT_STALE recebida. Reconciliando com /api/me...", b.ID())
			b.mu.Lock()
			if len(res.SaveOwnedFields) > 0 {
				b.saveOwnedFields = res.SaveOwnedFields
			}
			b.mu.Unlock()

			me, errMe := b.client.FetchMe(ctx)
			if errMe == nil && me != nil {
				b.mu.Lock()
				b.rawSave = me.Save
				b.saveVersion = me.SaveVersion
				b.clientVersion = me.ClientVersion
				if len(me.SaveOwnedFields) > 0 {
					b.saveOwnedFields = me.SaveOwnedFields
				}
				b.extractGameStateLocked()
				env.Save = b.rawSave
				env.BaseVersion = me.SaveVersion
				env.BaseClientVersion = me.ClientVersion
				env.Omitted = b.saveOwnedFields
				b.mu.Unlock()

				res2, err2 := b.client.PushSave(ctx, env)
				if err2 == nil && res2 != nil && res2.Code != "SAVE_CONTRACT_STALE" {
					b.mu.Lock()
					if res2.Version > 0 {
						b.saveVersion = res2.Version
					}
					if res2.ClientVersion > 0 {
						b.clientVersion = res2.ClientVersion
					}
					b.mu.Unlock()
					b.cb.RecordSuccess()
					log.Printf("[%s] ✅ Save re-enviado com sucesso após reconciliação de contrato!", b.ID())
					return
				}
			}
			return
		}

		b.mu.Lock()
		if res.Version > 0 {
			b.saveVersion = res.Version
		}
		if res.ClientVersion > 0 {
			b.clientVersion = res.ClientVersion
		}
		if res.Save != nil && len(res.Save) > 0 {
			b.rawSave = res.Save
			b.extractGameStateLocked()
		}
		b.mu.Unlock()

		b.cb.RecordSuccess()
	}(envelope)
}

// extractGameStateLocked popula o estado tipado Go a partir do JSON bruto do servidor
func (b *Bot) extractGameStateLocked() {
	if b.rawSave == nil {
		return
	}

	if lv := toFloat(b.rawSave["trainerLevel"]); lv > 0 {
		b.trainerLevel = int(lv)
	} else if b.trainerLevel <= 0 {
		b.trainerLevel = 1
	}

	if exp := toFloat(b.rawSave["trainerExp"]); exp > 0 {
		b.trainerExp = int64(exp)
	}

	if val, exists := b.rawSave["staminaMs"]; exists {
		b.staminaMs = int64(toFloat(val))
	} else if b.staminaMs <= 0 {
		b.staminaMs = MaxStaminaMs
	}

	if m := toFloat(b.rawSave["money"]); m > 0 {
		b.money = int64(m)
	}

	if st, ok := b.rawSave["stats"].(map[string]any); ok {
		if k := toFloat(st["kills"]); k > 0 {
			b.totalKills = int(k)
		}
		if c := toFloat(st["catches"]); c > 0 {
			b.totalCatches = int(c)
		}
	}

	// Extrai progresso da Pokedex / Bestiário
	b.pokedexCaught = make(map[int]bool)
	if pDex, ok := b.rawSave["pokedex"].(map[string]any); ok {
		if caughtMap, ok := pDex["caught"].(map[string]any); ok {
			for spIDStr := range caughtMap {
				var id int
				if _, err := fmt.Sscanf(spIDStr, "%d", &id); err == nil {
					b.pokedexCaught[id] = true
				}
			}
		}
	}

	// Extrai time de pokémons
	b.team = make([]Pokemon, 0)
	if teamArr, ok := b.rawSave["team"].([]any); ok {
		for _, item := range teamArr {
			if monMap, ok := item.(map[string]any); ok {
				sp, _ := GetSpecies(int(toFloat(monMap["speciesId"])))
				monName := fmt.Sprintf("%v", monMap["name"])
				if monName == "" || monName == "<nil>" {
					monName = sp.Name
				}
				if monName == "" {
					monName = fmt.Sprintf("Pokémon #%d", int(toFloat(monMap["speciesId"])))
				}

				monLevel := int(toFloat(monMap["level"]))
				if monLevel <= 0 {
					monLevel = b.trainerLevel
				}

				monHP := int(toFloat(monMap["hp"]))
				maxHP := int(toFloat(monMap["maxHp"]))
				if maxHP <= 0 {
					maxHP = monHP
				}
				if maxHP <= 0 {
					maxHP = 80 + (b.trainerLevel * 20)
				}
				if monHP <= 0 {
					monHP = maxHP
				}

				monMoves := GetSpeciesMoves(int(toFloat(monMap["speciesId"])))
				mon := Pokemon{
					UID:       fmt.Sprintf("%v", monMap["uid"]),
					Name:      monName,
					SpeciesID: int(toFloat(monMap["speciesId"])),
					Level:     monLevel,
					HP:        monHP,
					MaxHP:     maxHP,
					Moves:     monMoves,
				}
				b.team = append(b.team, mon)
			}
		}
	}

	// Define pokémon ativo
	if len(b.team) > 0 {
		b.activeMon = b.team[0]
	} else {
		b.activeMon = Pokemon{
			UID:       "default-pika",
			SpeciesID: 25,
			Name:      "Pikachu",
			Level:     10,
			HP:        150,
			MaxHP:     150,
		}
		b.team = append(b.team, b.activeMon)
	}

	b.status.Level = b.trainerLevel
	b.status.HP = b.activeMon.HP
	b.status.MaxHP = b.activeMon.MaxHP
	b.status.Gold = b.money
	b.status.Kills = b.totalKills
}

// selectOptimalRouteLocked resolve a melhor rota de EXP ou de Bestiário (Pilar 1 e Foco Bestiário)
func (b *Bot) selectOptimalRouteLocked() {
	if b.config.Settings.HuntTarget != "" && b.config.Settings.HuntTarget != "auto" && b.config.Settings.HuntTarget != "pokedex" {
		if spot, ok := FindSpot(b.config.Settings.HuntTarget); ok {
			b.currentSpot = spot
			b.currentRoute = spot.ID
			b.status.CurrentArea = spot.Name
			return
		}
	}

	// 1. Prioridade Bestiário: se FocusPokedex estiver ativo ou huntTarget == "pokedex"
	if b.config.Settings.FocusPokedex || b.config.Settings.HuntTarget == "pokedex" {
		bestSpot, missing := BestRouteForPokedex(b.pokedexCaught, b.trainerLevel)
		if missing > 0 && bestSpot.ID != "" {
			b.currentSpot = bestSpot
			b.currentRoute = bestSpot.ID
			b.status.CurrentArea = fmt.Sprintf("%s [Bestiário: %d faltam]", bestSpot.Name, missing)
			return
		}
	}

	// 2. Prioridade Eficiência & Fraquezas: Coleta tipos dos pokémons no time
	teamTypes := []string{"normal"}
	for _, mon := range b.team {
		if sp, ok := GetSpecies(mon.SpeciesID); ok && len(sp.Types) > 0 {
			teamTypes = append(teamTypes, sp.Types...)
		}
	}

	best := BestRouteForTeam(teamTypes, b.trainerLevel)
	b.currentSpot = best
	b.currentRoute = best.ID
	b.status.CurrentArea = best.Name
}

func toFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

func (b *Bot) GetStatus() engine.AccountStatus {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.status
}

func (b *Bot) ExecuteAction(action string, payload map[string]any) (any, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	switch action {
	case "autoHunt":
		b.isHunting = !b.isHunting
		b.config.Settings.AutoHunt = b.isHunting
		if b.isHunting {
			b.status.Status = "hunting"
			b.status.Logs = append(b.status.Logs, "Auto-hunt reativado.")
		} else {
			b.status.Status = "idle"
			b.status.Logs = append(b.status.Logs, "Auto-hunt pausado.")
		}
		return map[string]any{"autoHunt": b.isHunting}, nil

	case "autoHeal":
		b.activeMon.HP = b.activeMon.MaxHP
		for i := range b.team {
			b.team[i].HP = b.team[i].MaxHP
		}
		b.status.HP = b.activeMon.HP
		b.status.Logs = append(b.status.Logs, "Time curado com sucesso.")
		return "Time curado!", nil

	case "changeRoute":
		if target, ok := payload["target"].(string); ok && target != "" {
			if spot, found := FindSpot(target); found {
				b.currentSpot = spot
				b.currentRoute = spot.ID
				b.status.CurrentArea = spot.Name
				b.status.Logs = append(b.status.Logs, fmt.Sprintf("Rota alterada para: %s", spot.Name))
				return spot.Name, nil
			}
		}
		return nil, fmt.Errorf("rota não encontrada no catálogo")

	case "claimDaily":
		res, err := b.client.ClaimDailyReward(context.Background())
		if err != nil {
			return nil, err
		}
		b.status.Logs = append(b.status.Logs, "Recompensa diária resgatada.")
		return res, nil

	default:
		return nil, fmt.Errorf("ação desconhecida: %s", action)
	}
}

func (b *Bot) SupplyTicket(ticket, wsURL string) error {
	select {
	case b.ticketChan <- ticket:
	default:
	}
	return nil
}

func (b *Bot) SupplyTurnstileToken(token string) error {
	select {
	case b.turnstileChan <- token:
	default:
	}
	return nil
}

func (b *Bot) updateStatus(fn func(s *engine.AccountStatus)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	fn(&b.status)
	b.status.UpdatedAt = time.Now().UnixMilli()
}

func (b *Bot) addLogLocked(msg string) {
	b.status.Logs = append(b.status.Logs, msg)
	if len(b.status.Logs) > 10 {
		b.status.Logs = b.status.Logs[1:]
	}
}

func (b *Bot) addLog(msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.addLogLocked(msg)
}

// UserID retorna o ID oficial da conta no PokeLegacy
func (b *Bot) UserID() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.user.GetID()
}

// TrainerLevel retorna o nível atual do treinador
func (b *Bot) TrainerLevel() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.trainerLevel
}

// InParty indica se o bot está atualmente dentro de um grupo cooperativo
func (b *Bot) InParty() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.isInParty
}

// PartyID retorna o identificador do grupo atual
func (b *Bot) PartyID() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.partyID
}

// CreateParty solicita a criação de uma nova party via WebSocket
func (b *Bot) CreateParty() error {
	if b.socket == nil {
		return fmt.Errorf("socket offline")
	}
	b.addLog("👥 Criando nova party cooperativa...")
	return b.socket.Emit("party:create", nil)
}

// InviteToParty convida outro treinador para a party pelo UserID
func (b *Bot) InviteToParty(toUserID string) error {
	if b.socket == nil {
		return fmt.Errorf("socket offline")
	}
	b.addLog(fmt.Sprintf("👥 Convidando treinador #%s para a party...", toUserID))
	return b.socket.Emit("party:invite", map[string]any{"toUserId": toUserID})
}

// LeaveParty sai da party atual
func (b *Bot) LeaveParty() error {
	if b.socket == nil {
		return fmt.Errorf("socket offline")
	}
	return b.socket.Emit("party:leave", nil)
}

// TeamUIDs retorna a lista de identificadores únicos dos pokémons no time
func (b *Bot) TeamUIDs() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	uids := make([]string, 0, len(b.team))
	for _, m := range b.team {
		if m.UID != "" && m.UID != "<nil>" {
			uids = append(uids, m.UID)
		}
	}
	return uids
}

// IsPartyLeader indica se o bot é o líder da party ativa
func (b *Bot) IsPartyLeader() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.isInParty && b.partyLeaderID == b.user.GetID()
}

// PartyMembersCount retorna a quantidade de membros no grupo
func (b *Bot) PartyMembersCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.partyMembers)
}

// ProposeHunt propõe uma caçada coletiva na rota indicada
func (b *Bot) ProposeHunt(huntID string) error {
	if b.socket == nil {
		return fmt.Errorf("socket offline")
	}
	uids := b.TeamUIDs()
	b.addLog(fmt.Sprintf("🗺 Propondo caçada em grupo na rota %s...", huntID))
	return b.socket.Emit("party:huntPropose", map[string]any{
		"huntId":   huntID,
		"teamUids": uids,
	})
}

