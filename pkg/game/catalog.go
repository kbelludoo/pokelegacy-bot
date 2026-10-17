package game

import (
	_ "embed"
	"encoding/json"
	"log"
	"sync"
)

//go:embed catalog.json
var catalogJSON []byte

type Spot struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Region    string `json:"region"`
	Unlock    int    `json:"unlock"`
	TypeGroup string `json:"typeGroup"`
	Species   []int  `json:"species"`
}

type SpeciesInfo struct {
	ID      int      `json:"id"`
	Name    string   `json:"name"`
	Types   []string `json:"types"`
	BaseExp int      `json:"baseExp"`
}

type CatalogData struct {
	Spots   []Spot                 `json:"spots"`
	Species map[string]SpeciesInfo `json:"species"`
}

var (
	catalogOnce sync.Once
	catalog     CatalogData
	spotIndex   map[string]Spot
)

func initCatalog() {
	catalogOnce.Do(func() {
		spotIndex = make(map[string]Spot)
		if len(catalogJSON) == 0 {
			log.Println("[Catalog] Aviso: catalog.json vazio.")
			return
		}
		if err := json.Unmarshal(catalogJSON, &catalog); err != nil {
			log.Printf("[Catalog] Erro ao carregar catalog.json: %v\n", err)
			return
		}
		for _, s := range catalog.Spots {
			spotIndex[s.ID] = s
		}
		log.Printf("[Catalog] Carregados %d spots e %d espécies de Pokémon.\n", len(catalog.Spots), len(catalog.Species))
	})
}

// GetCatalog inicializa e devolve o catálogo
func GetCatalog() *CatalogData {
	initCatalog()
	return &catalog
}

// FindSpot busca um spot pelo ID
func FindSpot(id string) (Spot, bool) {
	initCatalog()
	s, ok := spotIndex[id]
	return s, ok
}

// GetSpecies busca espécie pelo ID
func GetSpecies(id int) (SpeciesInfo, bool) {
	initCatalog()
	info, ok := catalog.Species[string(rune(id))]
	if !ok {
		// Tenta via conversão string direta
		for _, sp := range catalog.Species {
			if sp.ID == id {
				return sp, true
			}
		}
	}
	return info, ok
}

// TypeEffectiveness retorna o multiplicador de dano elemental (2.0 = super efetivo, 0.5 = pouco efetivo, 0.0 = imune, 1.0 = normal)
func TypeEffectiveness(attackerType, defenderType string) float64 {
	switch attackerType {
	case "water":
		if defenderType == "fire" || defenderType == "ground" || defenderType == "rock" {
			return 2.0
		}
		if defenderType == "water" || defenderType == "grass" || defenderType == "dragon" {
			return 0.5
		}
	case "fire":
		if defenderType == "grass" || defenderType == "ice" || defenderType == "bug" || defenderType == "steel" {
			return 2.0
		}
		if defenderType == "fire" || defenderType == "water" || defenderType == "rock" || defenderType == "dragon" {
			return 0.5
		}
	case "grass":
		if defenderType == "water" || defenderType == "ground" || defenderType == "rock" {
			return 2.0
		}
		if defenderType == "fire" || defenderType == "grass" || defenderType == "poison" || defenderType == "flying" || defenderType == "bug" || defenderType == "dragon" || defenderType == "steel" {
			return 0.5
		}
	case "electric":
		if defenderType == "water" || defenderType == "flying" {
			return 2.0
		}
		if defenderType == "electric" || defenderType == "grass" || defenderType == "dragon" {
			return 0.5
		}
		if defenderType == "ground" {
			return 0.0
		}
	case "ground":
		if defenderType == "fire" || defenderType == "electric" || defenderType == "poison" || defenderType == "rock" || defenderType == "steel" {
			return 2.0
		}
		if defenderType == "grass" || defenderType == "bug" {
			return 0.5
		}
		if defenderType == "flying" {
			return 0.0
		}
	case "rock":
		if defenderType == "fire" || defenderType == "ice" || defenderType == "flying" || defenderType == "bug" {
			return 2.0
		}
		if defenderType == "fighting" || defenderType == "ground" || defenderType == "steel" {
			return 0.5
		}
	case "flying":
		if defenderType == "grass" || defenderType == "fighting" || defenderType == "bug" {
			return 2.0
		}
		if defenderType == "electric" || defenderType == "rock" || defenderType == "steel" {
			return 0.5
		}
	case "psychic":
		if defenderType == "fighting" || defenderType == "poison" {
			return 2.0
		}
		if defenderType == "psychic" || defenderType == "steel" {
			return 0.5
		}
		if defenderType == "dark" {
			return 0.0
		}
	case "fighting":
		if defenderType == "normal" || defenderType == "ice" || defenderType == "rock" || defenderType == "dark" || defenderType == "steel" {
			return 2.0
		}
		if defenderType == "poison" || defenderType == "flying" || defenderType == "psychic" || defenderType == "bug" || defenderType == "fairy" {
			return 0.5
		}
		if defenderType == "ghost" {
			return 0.0
		}
	case "bug":
		if defenderType == "grass" || defenderType == "psychic" || defenderType == "dark" {
			return 2.0
		}
		if defenderType == "fire" || defenderType == "fighting" || defenderType == "poison" || defenderType == "flying" || defenderType == "ghost" || defenderType == "steel" || defenderType == "fairy" {
			return 0.5
		}
	case "poison":
		if defenderType == "grass" || defenderType == "fairy" {
			return 2.0
		}
		if defenderType == "poison" || defenderType == "ground" || defenderType == "rock" || defenderType == "ghost" {
			return 0.5
		}
		if defenderType == "steel" {
			return 0.0
		}
	case "ice":
		if defenderType == "grass" || defenderType == "ground" || defenderType == "flying" || defenderType == "dragon" {
			return 2.0
		}
		if defenderType == "fire" || defenderType == "water" || defenderType == "ice" || defenderType == "steel" {
			return 0.5
		}
	case "dragon":
		if defenderType == "dragon" {
			return 2.0
		}
		if defenderType == "steel" {
			return 0.5
		}
		if defenderType == "fairy" {
			return 0.0
		}
	case "ghost":
		if defenderType == "psychic" || defenderType == "ghost" {
			return 2.0
		}
		if defenderType == "dark" {
			return 0.5
		}
		if defenderType == "normal" {
			return 0.0
		}
	case "dark":
		if defenderType == "psychic" || defenderType == "ghost" {
			return 2.0
		}
		if defenderType == "fighting" || defenderType == "dark" || defenderType == "fairy" {
			return 0.5
		}
	case "steel":
		if defenderType == "ice" || defenderType == "rock" || defenderType == "fairy" {
			return 2.0
		}
		if defenderType == "fire" || defenderType == "water" || defenderType == "electric" || defenderType == "steel" {
			return 0.5
		}
	case "fairy":
		if defenderType == "fighting" || defenderType == "dragon" || defenderType == "dark" {
			return 2.0
		}
		if defenderType == "fire" || defenderType == "poison" || defenderType == "steel" {
			return 0.5
		}
	}
	return 1.0
}

// BestRouteForTeam seleciona a melhor rota com base no nível do treinador e fraquezas elementais (Pilar 1)
func BestRouteForTeam(teamTypes []string, trainerLevel int) Spot {
	initCatalog()

	var bestSpot Spot
	bestScore := -1.0

	for _, spot := range catalog.Spots {
		// Rota deve estar liberada para o nível do jogador
		if spot.Unlock > trainerLevel {
			continue
		}

		// Rota dentro de faixa aceitável (não caçar em rotas muito abaixo se tiver opções melhores)
		levelDiff := trainerLevel - spot.Unlock
		if levelDiff > 35 && trainerLevel > 30 {
			// Dá prioridade para rotas mais recentes se existirem
			continue
		}

		// Calcula multiplicador elemental médio do nosso time contra o tipo do spot
		elementalMultiplier := 1.0
		for _, t := range teamTypes {
			mult := TypeEffectiveness(t, spot.TypeGroup)
			if mult > elementalMultiplier {
				elementalMultiplier = mult
			}
		}

		// Pontuação: favorece rotas de nível alto e com vantagem elemental 2.0x
		score := float64(spot.Unlock)*1.5 + (elementalMultiplier * 50.0)
		if score > bestScore {
			bestScore = score
			bestSpot = spot
		}
	}

	if bestSpot.ID == "" && len(catalog.Spots) > 0 {
		// Fallback: primeira rota disponível
		bestSpot = catalog.Spots[0]
	}

	return bestSpot
}

// BestRouteForPokedex seleciona a rota desbloqueada com o maior número de Pokémon ainda não registrados no Bestiário (Pilar Pokédex)
func BestRouteForPokedex(caughtSpecies map[int]bool, trainerLevel int) (Spot, int) {
	initCatalog()

	var bestSpot Spot
	maxMissing := 0

	for _, spot := range catalog.Spots {
		if spot.Unlock > trainerLevel {
			continue
		}

		missingInRoute := 0
		for _, spID := range spot.Species {
			if !caughtSpecies[spID] {
				missingInRoute++
			}
		}

		if missingInRoute > maxMissing {
			maxMissing = missingInRoute
			bestSpot = spot
		}
	}

	return bestSpot, maxMissing
}
