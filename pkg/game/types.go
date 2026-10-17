package game

import "fmt"

// AccountConfig modelo de configuração lido do accounts.json
type AccountConfig struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Enabled     bool            `json:"enabled"`
	Credentials GameCredentials `json:"credentials"`
	Settings    GameSettings    `json:"settings"`
	BindIP      string          `json:"bindIP,omitempty"` // Isolamento de IP local para VPS multi-IP (Pilar 5)
	Proxy       string          `json:"proxy,omitempty"`  // Suporte a Proxy HTTP/HTTPS/SOCKS5 por conta (ex: http://ip:port ou socks5://ip:port)
}

// GameCredentials credenciais de acesso ao PokeLegacy Online
type GameCredentials struct {
	Email         string `json:"email"`
	Username      string `json:"username,omitempty"`
	Password      string `json:"password"`
	SessionCookie string `json:"sessionCookie,omitempty"`
	Token         string `json:"token,omitempty"`         // Bearer token (poke_idle_token)
	DeviceID      string `json:"deviceId,omitempty"`      // UUID v4 para o cabeçalho X-Poke-Aparelho
}

// GameSettings preferências de automação do bot
type GameSettings struct {
	HuntTarget      string `json:"huntTarget"`                // "auto", "pokedex" ou ID de rota (ex: "kanto_campos_de_pallet")
	AutoHunt        bool   `json:"autoHunt"`                  // Caçar monstros e progredir
	AutoSell        bool   `json:"autoSell"`                  // Vender loot acumulado no NPC do shopping
	AutoHeal        bool   `json:"autoHeal"`                  // Curar pokémons ou voltar ao Pokécenter em HP baixo
	AutoDaily       bool   `json:"autoDaily"`                 // Resgatar recompensas diárias automaticamente
	AutoCatch       bool   `json:"autoCatch"`                 // Tentar capturar pokémons selvagens/shinies
	FocusPokedex    bool   `json:"focusPokedex"`              // Prioriza rotas e capturas de pokémons que faltam no Bestiário
	ExploitWeakness bool   `json:"exploitWeakness"`           // Troca para o Pokémon do time com vantagem de tipo (2.0x dano)
	PreferredBall   string `json:"preferredBall,omitempty"`   // "pokeball", "greatball", "ultraball"
	MinHpPercent    int    `json:"minHpPercent,omitempty"`    // Porcentagem de HP para acionar recuo/cura (padrão 25%)
	SaveIntervalSec int    `json:"saveIntervalSec,omitempty"` // Intervalo em segundos entre /api/save (padrão 30s)
	Lure            int    `json:"lure,omitempty"`
}

// Pokemon representa um monstro no time do jogador
type Pokemon struct {
	UID       string   `json:"uid"`
	SpeciesID int      `json:"speciesId"`
	Name      string   `json:"name"`
	Level     int      `json:"level"`
	Exp       int64    `json:"exp"`
	HP        int      `json:"hp"`
	MaxHP     int      `json:"maxHp,omitempty"`
	Shiny     bool     `json:"shiny"`
	Held      string   `json:"held,omitempty"`
	Moves     []string `json:"moves,omitempty"`
}

// LoginRequest corpo para POST /api/login
type LoginRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

// User dados retornados do usuário autenticado
type User struct {
	ID          any    `json:"id"`
	Email       string `json:"email,omitempty"`
	TrainerName string `json:"trainerName,omitempty"`
	VIP         bool   `json:"vip,omitempty"`
}

func (u User) GetID() string {
	if u.ID == nil {
		return ""
	}
	return fmt.Sprintf("%v", u.ID)
}

// LoginResponse retorno de POST /api/login
type LoginResponse struct {
	Token           string   `json:"token"`
	User            User     `json:"user"`
	SaveVersion     int      `json:"saveVersion"`
	ClientVersion   int      `json:"clientVersion"`
	SaveOwnedFields []string `json:"saveOwnedFields"`
	Error           string   `json:"error,omitempty"`
}

// MeResponse retorno de GET /api/me
type MeResponse struct {
	User            User           `json:"user"`
	Save            map[string]any `json:"save"`
	SaveVersion     int            `json:"saveVersion"`
	ClientVersion   int            `json:"clientVersion"`
	SaveOwnedFields []string       `json:"saveOwnedFields"`
	Token           string         `json:"token,omitempty"`
	Error           string         `json:"error,omitempty"`
}

// SaveEnvelope corpo enviado no POST /api/save (Preservação estrita de Schema - Pilar 3)
type SaveEnvelope struct {
	Save              map[string]any `json:"save"`
	AccountID         any            `json:"accountId"`
	BaseVersion       int            `json:"baseVersion"`
	BaseClientVersion int            `json:"baseClientVersion,omitempty"`
	Omitted           []string       `json:"omitted,omitempty"`
}

// SaveResponse retorno de POST /api/save
type SaveResponse struct {
	Version       int            `json:"version"`
	ClientVersion int            `json:"clientVersion"`
	Save          map[string]any `json:"save,omitempty"`
	Error         string         `json:"error,omitempty"`
}
