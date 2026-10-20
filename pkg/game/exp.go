package game

import (
	_ "embed"
	"encoding/json"
	"log"
	"sync"
)

//go:embed xp_table.json
var xpTableJSON []byte

var (
	xpTableOnce sync.Once
	xpTable     []int64
)

func initXPTable() {
	xpTableOnce.Do(func() {
		if len(xpTableJSON) == 0 {
			log.Println("[EXP] Aviso: xp_table.json vazio.")
			return
		}
		if err := json.Unmarshal(xpTableJSON, &xpTable); err != nil {
			log.Printf("[EXP] Erro ao carregar xp_table.json: %v\n", err)
			return
		}
	})
}

// ExpToLevel calcula o nível exato do treinador baseado no total acumulado de EXP
// usando o mesmo algoritmo de busca binária do cliente oficial (function yn no core)
func ExpToLevel(exp int64) int {
	initXPTable()
	if len(xpTable) == 0 {
		return 1
	}
	if exp <= 0 {
		return 1
	}

	maxLevel := len(xpTable) - 1
	low := 1
	high := maxLevel

	for low < high {
		mid := (low + high + 1) / 2
		if xpTable[mid] <= exp {
			low = mid
		} else {
			high = mid - 1
		}
	}
	return low
}

// ExpForLevel retorna o montante total acumulado de EXP necessário para atingir o nível especificado
func ExpForLevel(level int) int64 {
	initXPTable()
	if len(xpTable) == 0 || level <= 1 {
		return 0
	}
	if level >= len(xpTable) {
		return xpTable[len(xpTable)-1]
	}
	return xpTable[level]
}

// ExpForNextLevel retorna o EXP total para o próximo nível
func ExpForNextLevel(currentLevel int) int64 {
	return ExpForLevel(currentLevel + 1)
}
