package game

import (
	"context"
	"log"
	"sort"
	"sync"
	"time"
)

// PartyCoordinator coordena automaticamente a formação e manutenção de grupos (party)
// entre as contas ativas respeitando a regra oficial do PokeLegacy (diferença <= 25 níveis do membro mais forte)
type PartyCoordinator struct {
	mu               sync.Mutex
	bots             []*Bot
	lastHuntProp     time.Time
	lastPartyAttempt time.Time
}

// NewPartyCoordinator inicializa o coordenador de Party
func NewPartyCoordinator() *PartyCoordinator {
	return &PartyCoordinator{
		bots: make([]*Bot, 0),
	}
}

// Register adiciona um bot ao pool gerenciado pelo coordenador
func (pc *PartyCoordinator) Register(b *Bot) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.bots = append(pc.bots, b)
}

// Start inicia o loop de monitoramento de níveis e convites automáticos
func (pc *PartyCoordinator) Start(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pc.evaluateAndFormParties()
			}
		}
	}()
}

func (pc *PartyCoordinator) evaluateAndFormParties() {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	if len(pc.bots) < 2 {
		return
	}

	type candidate struct {
		bot   *Bot
		id    string
		level int
		name  string
	}

	var candidates []candidate
	for _, b := range pc.bots {
		uID := b.UserID()
		lv := b.TrainerLevel()
		if uID != "" && lv > 0 {
			candidates = append(candidates, candidate{
				bot:   b,
				id:    uID,
				level: lv,
				name:  b.Name(),
			})
		}
	}

	if len(candidates) < 2 {
		return
	}

	// Ordena candidatos por nível ascendente
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].level < candidates[j].level
	})

	// Encontra a maior janela de candidatos compatíveis onde maxLevel - minLevel <= 25
	// e tamanho máximo <= 5 (limite oficial do PokeLegacy)
	var bestCluster []candidate

	for i := 0; i < len(candidates); i++ {
		var cluster []candidate
		cluster = append(cluster, candidates[i])

		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].level-candidates[i].level <= 25 {
				cluster = append(cluster, candidates[j])
				if len(cluster) >= 5 {
					break
				}
			} else {
				break
			}
		}

		if len(cluster) > len(bestCluster) {
			bestCluster = cluster
		}
	}

	if len(bestCluster) < 2 {
		return
	}

	// Escolhe o membro de maior nível do cluster para ser o líder (ele cria e convida)
	leaderIdx := len(bestCluster) - 1
	leader := bestCluster[leaderIdx]
	leaderBot := leader.bot

	// 1. Cria a party se o líder ainda não estiver em uma (com cooldown de 2m para evitar spam se a feature estiver desativada no servidor)
	if !leaderBot.InParty() {
		if time.Since(pc.lastPartyAttempt) < 2*time.Minute {
			return
		}
		pc.lastPartyAttempt = time.Now()
		log.Printf("[PartyCoordinator] 👥 Níveis compatíveis detectados (%d contas, Níveis %d-%d)! Tentando formar party (Líder: %s Lv. %d)...",
			len(bestCluster), bestCluster[0].level, leader.level, leader.name, leader.level)
		_ = leaderBot.CreateParty()
		time.Sleep(1 * time.Second)
	}

	// 2. Se a party do líder foi criada e está ativa, convida os membros restantes do cluster
	if leaderBot.InParty() {
		for idx, member := range bestCluster {
			if idx == leaderIdx {
				continue
			}
			if !member.bot.InParty() {
				log.Printf("[PartyCoordinator] 👥 %s (Lv. %d) convidando %s (Lv. %d) para a party...",
					leader.name, leader.level, member.name, member.level)
				_ = leaderBot.InviteToParty(member.id)
				time.Sleep(500 * time.Millisecond)
			}
		}
	}

	// 3. Se a party tem pelo menos 2 membros conectados, o líder propõe a rota ideal a cada 45s
	if leaderBot.InParty() && leaderBot.PartyMembersCount() >= 2 {
		if time.Since(pc.lastHuntProp) >= 45*time.Second {
			pc.lastHuntProp = time.Now()
			// Rota adequada ao menor nível do cluster para garantir 100% de acesso sem restrições
			lowestLevel := bestCluster[0].level
			targetSpot := BestRouteForTeam([]string{"normal"}, lowestLevel)
			if targetSpot.ID != "" {
				log.Printf("[PartyCoordinator] 🗺 Líder %s propondo caçada em grupo na rota %s (%s) para a party...",
					leader.name, targetSpot.ID, targetSpot.Name)
				_ = leaderBot.ProposeHunt(targetSpot.ID)
			}
		}
	}
}
