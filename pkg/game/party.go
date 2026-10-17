package game

import (
	"context"
	"log"
	"math"
	"sync"
	"time"
)

// PartyCoordinator coordena automaticamente a formação e manutenção de grupos (party)
// entre as contas ativas cujos níveis de treinador sejam equivalentes (diferença <= 25 níveis)
type PartyCoordinator struct {
	mu   sync.Mutex
	bots []*Bot
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
	ticker := time.NewTicker(10 * time.Second)
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

	// Procura o maior grupo compatível onde todos tenham no máximo 25 níveis de diferença
	for i := 0; i < len(candidates); i++ {
		leader := candidates[i]
		var cluster []candidate
		cluster = append(cluster, leader)

		for j := 0; j < len(candidates); j++ {
			if i == j {
				continue
			}
			other := candidates[j]
			compatible := true
			for _, m := range cluster {
				if math.Abs(float64(m.level-other.level)) > 25 {
					compatible = false
					break
				}
			}
			if compatible {
				cluster = append(cluster, other)
				if len(cluster) >= 5 { // Limite máximo da party no PokeLegacy = 5 membros
					break
				}
			}
		}

		if len(cluster) >= 2 {
			leaderBot := cluster[0].bot
			if !leaderBot.InParty() {
				log.Printf("[PartyCoordinator] 👥 Níveis equivalentes detectados! Formando party para %d contas (Líder: %s Lv. %d)...",
					len(cluster), cluster[0].name, cluster[0].level)
				_ = leaderBot.CreateParty()
				time.Sleep(1 * time.Second)
			}

			// Se o líder está com a party ativa, convida os demais membros do cluster
			for _, member := range cluster[1:] {
				if !member.bot.InParty() {
					log.Printf("[PartyCoordinator] 👥 %s (Lv. %d) convidando %s (Lv. %d, ID: %s) para a party...",
						cluster[0].name, cluster[0].level, member.name, member.level, member.id)
					_ = leaderBot.InviteToParty(member.id)
					time.Sleep(500 * time.Millisecond)
				}
			}
			break
		}
	}
}
