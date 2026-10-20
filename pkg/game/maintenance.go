package game

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// mergeOfficialLocked mescla os campos oficiais retornados por endpoints parciais e atualiza o estado
func (b *Bot) mergeOfficialLocked(official map[string]any, saveVersion int) {
	if official == nil || b.rawSave == nil {
		return
	}
	for k, v := range official {
		b.rawSave[k] = v
	}
	if saveVersion > 0 {
		b.saveVersion = saveVersion
	}
	b.extractGameStateLocked()
}

// isEssentialItem identifica itens que não devem ser vendidos ou guardados no depósito
func isEssentialItem(id string) bool {
	switch id {
	case "poke_ball", "great_ball", "super_ball", "ultra_ball", "master_ball", "premier_ball", "safari_ball",
		"ultra_ball_conta", "great_ball_conta", "super_ball_conta", "poke_ball_conta",
		"potion", "super_potion", "hyper_potion", "max_potion",
		"hyper_potion_conta", "super_potion_conta", "potion_conta",
		"revive", "max_revive", "revive_conta",
		"xp_boost_ticket", "shiny_boost_ticket", "loot_boost_ticket",
		"rare_candy", "shiny_ticket", "poke_bag", "mystery_bag",
		"devoted_token":
		return true
	}
	if strings.HasPrefix(id, "held_") || strings.HasPrefix(id, "held") {
		return true
	}
	return false
}

// isLootItem identifica itens comuns de despojo de caça (troféus vendíveis)
func isLootItem(id string) bool {
	switch id {
	case "rat_tail", "nail", "rubber_ball", "bitten_apple", "small_wing", "wool_ball",
		"feather", "fur", "horn", "seed", "leaf", "shell", "bone", "scale", "fang",
		"string", "poison_sting", "sharp_beak", "hard_stone", "soft_sand", "mystic_water",
		"charcoal", "miracle_seed", "black_glasses", "magnet", "twisted_spoon", "dragon_fang",
		"silk", "insect_eye", "claw", "fin", "antenna", "crystal":
		return true
	}
	return false
}

// runMaintenance executa todas as rotinas automatizadas do jogo com estrito anti-ban
func (b *Bot) runMaintenance(ctx context.Context) {
	if b.cb == nil || !b.cb.AllowAction() {
		return
	}

	// 0. Sincronização e Consolidação Canônica de Progresso via WebSocket
	if b.socket != nil {
		_ = b.socket.ResumeHunt()
	}

	// 1. Brinde Diário e Welcome VIP
	b.mu.RLock()
	dailyRewardMap, _ := b.rawSave["dailyReward"].(map[string]any)
	b.mu.RUnlock()

	todayKey := time.Now().Format("2006-01-02")
	lastClaimDay, _ := dailyRewardMap["lastClaimDay"].(string)
	if lastClaimDay != todayKey {
		if res, err := b.client.ClaimDailyReward(ctx); err == nil {
			b.addLog("🎁 Brinde diário resgatado com sucesso!")
			b.mu.Lock()
			if off, ok := res["official"].(map[string]any); ok {
				b.mergeOfficialLocked(off, int(toFloat(res["saveVersion"])))
			}
			b.mu.Unlock()
		}
	}
	_ = b.client.AcknowledgeWelcomeVip(ctx)

	// 2. Missões Diárias (Auto-escolha e resgate de concluídas)
	if missaoRes, err := b.client.FetchDailyMissions(ctx); err == nil {
		if missaoData, ok := missaoRes["missao"].(map[string]any); ok {
			if objs, ok := missaoData["objetivos"].([]any); ok {
				for _, objAny := range objs {
					if obj, ok := objAny.(map[string]any); ok {
						id, _ := obj["id"].(string)
						escolhida, _ := obj["escolhida"].(bool)
						concluido, _ := obj["concluido"].(bool)
						resgatado, _ := obj["resgatado"].(bool)

						if !escolhida {
							diff := "dificil"
							if id == "captura" {
								diff = "facil"
							}
							if _, err := b.client.ChooseDailyMission(ctx, id, diff); err == nil {
								b.addLog(fmt.Sprintf("📜 Missão diária configurada: %s (%s)", id, diff))
							}
						} else if concluido && !resgatado {
							if res, err := b.client.ClaimDailyMission(ctx, id); err == nil {
								b.addLog(fmt.Sprintf("🏆 Prêmio da missão diária resgatado: %s!", id))
								b.mu.Lock()
								if off, ok := res["official"].(map[string]any); ok {
									b.mergeOfficialLocked(off, int(toFloat(res["saveVersion"])))
								}
								b.mu.Unlock()
							}
						}
					}
				}
			}
		}
	}

	// 3. Jornada do Treinador (Avanço automático e resgate de marcos)
	for i := 0; i < 5; i++ {
		jornadaRes, err := b.client.FetchJornada(ctx)
		if err != nil {
			break
		}
		jornadaData, ok := jornadaRes["jornada"].(map[string]any)
		if !ok {
			break
		}
		progressMade := false

		// Check submissao
		if sub, ok := jornadaData["submissao"].(map[string]any); ok && sub != nil {
			subId, _ := sub["id"].(string)
			aceitaVisto, _ := sub["aceitaVisto"].(bool)
			visto, _ := sub["visto"].(bool)
			cumprida, _ := sub["cumprida"].(bool)

			if aceitaVisto && !visto && subId != "" {
				if _, err := b.client.MarkJornadaSeen(ctx, subId); err == nil {
					b.addLog(fmt.Sprintf("🗺️ Jornada: marco visto '%s'", subId))
					progressMade = true
					time.Sleep(400 * time.Millisecond)
				}
			} else if cumprida && subId != "" {
				if res, err := b.client.ClaimJornada(ctx, subId); err == nil {
					b.addLog(fmt.Sprintf("🎉 Jornada: submilenar '%s' resgatado!", subId))
					b.mu.Lock()
					if off, ok := res["official"].(map[string]any); ok {
						b.mergeOfficialLocked(off, int(toFloat(res["saveVersion"])))
					}
					b.mu.Unlock()
					progressMade = true
					time.Sleep(400 * time.Millisecond)
				}
			}
		}

		// Check missao principal
		if mis, ok := jornadaData["missao"].(map[string]any); ok && mis != nil {
			misId, _ := mis["id"].(string)
			cumprida, _ := mis["cumprida"].(bool)
			if cumprida && misId != "" {
				if res, err := b.client.ClaimJornada(ctx, misId); err == nil {
					b.addLog(fmt.Sprintf("🌟 Jornada: missão '%s' concluída e resgatada!", misId))
					b.mu.Lock()
					if off, ok := res["official"].(map[string]any); ok {
						b.mergeOfficialLocked(off, int(toFloat(res["saveVersion"])))
					}
					b.mu.Unlock()
					progressMade = true
					time.Sleep(400 * time.Millisecond)
				}
			}
		}

		if !progressMade {
			break
		}
	}

	// 4. Mochila e Consumíveis Automáticos
	b.mu.RLock()
	invMap, _ := b.rawSave["inventory"].(map[string]any)
	storeBoosts, _ := b.rawSave["storeBoosts"].(map[string]any)
	xpBoostMap, _ := b.rawSave["xpBoost"].(map[string]any)
	b.mu.RUnlock()

	if invMap != nil {
		// XP Boost Ticket (+50% EXP por 1h)
		xpTickets := int(toFloat(invMap["xp_boost_ticket"]))
		var xpActive bool
		if storeBoosts != nil {
			if xpB, ok := storeBoosts["xp"].(map[string]any); ok {
				if toFloat(xpB["remainingMs"]) > 0 {
					xpActive = true
				}
			}
		}
		if xpBoostMap != nil && !xpActive {
			if toFloat(xpBoostMap["remainingMs"]) > 0 {
				xpActive = true
			}
		}
		if xpTickets > 0 && !xpActive {
			if res, err := b.client.UseXpBoostTicket(ctx); err == nil {
				b.addLog("⚡ EXP BOOST: Bilhete de +50% EXP ativado por 1h!")
				b.mu.Lock()
				if off, ok := res["official"].(map[string]any); ok {
					b.mergeOfficialLocked(off, int(toFloat(res["saveVersion"])))
				}
				b.mu.Unlock()
			}
		}

		// Shiny Boost Ticket (+75% shiny rate por 1h)
		shinyTickets := int(toFloat(invMap["shiny_boost_ticket"]))
		var shinyActive bool
		if storeBoosts != nil {
			if shB, ok := storeBoosts["shiny"].(map[string]any); ok {
				if toFloat(shB["remainingMs"]) > 0 {
					shinyActive = true
				}
			}
		}
		if shinyTickets > 0 && !shinyActive {
			if res, err := b.client.UseShinyTicket(ctx); err == nil {
				b.addLog("✨ SHINY BOOST: Bilhete de +75% taxa de Shiny ativado por 1h!")
				b.mu.Lock()
				if off, ok := res["official"].(map[string]any); ok {
					b.mergeOfficialLocked(off, int(toFloat(res["saveVersion"])))
				}
				b.mu.Unlock()
			}
		}

		// Pokebag
		pokeBags := int(toFloat(invMap["poke_bag"]))
		if pokeBags > 0 {
			if res, err := b.client.OpenPokeBag(ctx); err == nil {
				b.addLog("🎁 Poke Mystery Bag aberta com sucesso!")
				b.mu.Lock()
				if off, ok := res["official"].(map[string]any); ok {
					b.mergeOfficialLocked(off, int(toFloat(res["saveVersion"])))
				}
				b.mu.Unlock()
			}
		}

		// Rare Candies
		rareCandies := int(toFloat(invMap["rare_candy"]))
		if rareCandies > 0 {
			if res, err := b.client.UseRareCandy(ctx); err == nil {
				b.addLog("🍬 Doce Raro consumido com sucesso!")
				b.mu.Lock()
				if off, ok := res["official"].(map[string]any); ok {
					b.mergeOfficialLocked(off, int(toFloat(res["saveVersion"])))
				}
				b.mu.Unlock()
			}
		}
	}

	// 5. Auto-Restock de Pokébolas (se faltar e tiver ouro)
	b.mu.RLock()
	ballsCount := 0
	if invMap != nil {
		ballsCount = int(toFloat(invMap["poke_ball"]))
	}
	curMoney := b.money
	b.mu.RUnlock()

	if ballsCount < 50 && curMoney >= 20000 {
		if res, err := b.client.BuyShopItem(ctx, "poke_ball", 100); err == nil {
			b.addLog("◓ Loja: 100 Pokébolas compradas automaticamente para manter caçada.")
			b.mu.Lock()
			if m, ok := res["money"].(float64); ok {
				b.money = int64(m)
			}
			if ow, ok := res["owned"].(float64); ok && invMap != nil {
				invMap["poke_ball"] = ow
			}
			b.mu.Unlock()
		}
	}

	// 6. Oficina Artesanal (Coleta passiva)
	_, _ = b.client.CollectOficina(ctx)

	// 7. Doação para Buffs Globais (apenas com reserva > 200.000 moedas e a cada 2h)
	if curMoney > 200000 && time.Since(b.lastDonationAt) >= 2*time.Hour {
		if gbRes, err := b.client.FetchGlobalBuffs(ctx); err == nil {
			if camps, ok := gbRes["campaigns"].([]any); ok {
				for _, cAny := range camps {
					if camp, ok := cAny.(map[string]any); ok {
						st, _ := camp["status"].(string)
						bf, _ := camp["buff"].(string)
						cid := int(toFloat(camp["campaignId"]))
						if st == "funding" && (bf == "xp" || bf == "shiny" || bf == "loot") {
							if res, err := b.client.DonateGlobalBuff(ctx, bf, 5000, cid); err == nil {
								b.lastDonationAt = time.Now()
								b.addLog(fmt.Sprintf("🏛️ Global Buff: 5.000 moedas doadas para a campanha %s!", strings.ToUpper(bf)))
								b.mu.Lock()
								if m, ok := res["money"].(float64); ok {
									b.money = int64(m)
								} else {
									b.money -= 5000
								}
								b.mu.Unlock()
								break
							}
						}
					}
				}
			}
		}
	}

	// 8. Auto-Sell & Auto-Stash inteligente
	if b.config.Settings.AutoSell {
		b.mu.RLock()
		var itemsToSell []string
		var itemsToStash []string
		if invMap != nil {
			for itemId, qtyAny := range invMap {
				qty := int(toFloat(qtyAny))
				if qty <= 0 {
					continue
				}
				if isEssentialItem(itemId) {
					continue
				}
				if isLootItem(itemId) {
					itemsToSell = append(itemsToSell, itemId)
				} else {
					itemsToStash = append(itemsToStash, itemId)
				}
			}
		}
		b.mu.RUnlock()

		if len(itemsToSell) > 0 {
			if sRes, err := b.client.SellAll(ctx, itemsToSell); err == nil {
				tot := int(toFloat(sRes["total"]))
				b.addLog(fmt.Sprintf("💰 Loot vendido no shopping: +%d moedas obtidas.", tot))
				b.mu.Lock()
				if m, ok := sRes["money"].(float64); ok {
					b.money = int64(m)
				}
				b.mu.Unlock()
			}
		}

		if len(itemsToStash) > 0 {
			if _, err := b.client.StashAll(ctx, itemsToStash); err == nil {
				b.addLog(fmt.Sprintf("🎒 %d tipos de itens guardados com segurança no depósito.", len(itemsToStash)))
			}
		}
	}

	// 9. Auto-Equip de Held Items disponíveis na mochila
	b.mu.RLock()
	var heldToEquip string
	if invMap != nil {
		for itemId, qtyAny := range invMap {
			if strings.HasPrefix(itemId, "held_") && int(toFloat(qtyAny)) > 0 {
				heldToEquip = itemId
				break
			}
		}
	}
	var targetMonUid string
	if teamList, ok := b.rawSave["team"].([]any); ok && heldToEquip != "" {
		for _, monAny := range teamList {
			if monMap, ok := monAny.(map[string]any); ok {
				uid, _ := monMap["uid"].(string)
				heldX := monMap["heldX"]
				if uid != "" && (heldX == nil || heldX == "") {
					targetMonUid = uid
					break
				}
			}
		}
	}
	b.mu.RUnlock()

	if targetMonUid != "" && heldToEquip != "" {
		if res, err := b.client.EquipHeld(ctx, targetMonUid, heldToEquip); err == nil {
			b.addLog(fmt.Sprintf("🛡️ Held Item '%s' equipado com sucesso no Pokémon %s!", heldToEquip, targetMonUid))
			b.mu.Lock()
			if off, ok := res["official"].(map[string]any); ok {
				b.mergeOfficialLocked(off, int(toFloat(res["saveVersion"])))
			}
			b.mu.Unlock()
		}
	}
}
