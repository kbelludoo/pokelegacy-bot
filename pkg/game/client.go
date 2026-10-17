package game

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"bot-template-go/pkg/engine"
)

const (
	DefaultBaseURL       = "https://pokelegacyonline.com.br"
	DefaultClientRelease = "3b7b9e0c2ed34385984484fbf0c180aec317fc4a"
	DefaultUserAgent     = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

// LegacyClient encapsula as chamadas HTTP para o backend do PokeLegacy Online
type LegacyClient struct {
	baseURL       string
	client        *http.Client
	token         string
	deviceID      string
	clientRelease string
}

func generateUUIDv4() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// NewLegacyClient inicializa um cliente com suporte a bind de IP local ou Proxy (Pilar 5)
func NewLegacyClient(baseURL string, bindIP string, proxyURL string, deviceID string, timeout time.Duration) *LegacyClient {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	if deviceID == "" {
		deviceID = generateUUIDv4()
	}

	transport := &http.Transport{
		MaxIdleConns:        10,
		IdleConnTimeout:     30 * time.Second,
		DisableCompression: false,
	}

	if proxyURL != "" {
		if pURL, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(pURL)
		}
	}

	if bindIP != "" {
		localIP := net.ParseIP(bindIP)
		if localIP != nil {
			transport.DialContext = (&net.Dialer{
				LocalAddr: &net.TCPAddr{IP: localIP},
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext
		}
	}

	return &LegacyClient{
		baseURL:       baseURL,
		client:        &http.Client{Transport: transport, Timeout: timeout},
		deviceID:      deviceID,
		clientRelease: DefaultClientRelease,
	}
}

func (c *LegacyClient) SetToken(token string) {
	c.token = token
}

func (c *LegacyClient) Token() string {
	return c.token
}

func (c *LegacyClient) DeviceID() string {
	return c.deviceID
}

func (c *LegacyClient) doRequest(ctx context.Context, method, endpoint string, reqBody any, respTarget any) (int, string, error) {
	var bodyReader io.Reader
	if reqBody != nil {
		data, err := json.Marshal(reqBody)
		if err != nil {
			return 0, "", fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	url := c.baseURL + endpoint
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return 0, "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", DefaultUserAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "pt-BR,pt;q=0.9,en-US;q=0.8,en;q=0.7")
	req.Header.Set("Origin", c.baseURL)
	req.Header.Set("Referer", c.baseURL+"/jogar")
	req.Header.Set("X-Client-Release", c.clientRelease)
	req.Header.Set("X-Poke-Aparelho", c.deviceID)

	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("request %s failed: %w", endpoint, err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, "", fmt.Errorf("read response body: %w", err)
	}

	bodyStr := string(respBytes)

	// Análise rigorosa com Circuit Breaker anti-cheat
	if isRejection, reason := engine.IsAntiCheatRejection(resp.StatusCode, bodyStr); isRejection {
		return resp.StatusCode, bodyStr, fmt.Errorf("anti-cheat disparado: %s", reason)
	}

	if resp.StatusCode == 409 {
		if respTarget != nil && len(respBytes) > 0 {
			_ = json.Unmarshal(respBytes, respTarget)
		}
		return resp.StatusCode, bodyStr, fmt.Errorf("HTTP 409: %s", bodyStr)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, bodyStr, fmt.Errorf("HTTP %d em %s: %s", resp.StatusCode, endpoint, bodyStr)
	}

	if respTarget != nil && len(respBytes) > 0 {
		if err := json.Unmarshal(respBytes, respTarget); err != nil {
			return resp.StatusCode, bodyStr, fmt.Errorf("unmarshal response: %w", err)
		}
	}

	return resp.StatusCode, bodyStr, nil
}

// Login realiza a autenticação na API do jogo
func (c *LegacyClient) Login(ctx context.Context, login, password string) (*LoginResponse, error) {
	req := LoginRequest{
		Login:    login,
		Password: password,
	}
	var res LoginResponse
	_, _, err := c.doRequest(ctx, http.MethodPost, "/api/login", req, &res)
	if err != nil {
		return nil, err
	}
	if res.Token != "" {
		c.token = res.Token
	}
	return &res, nil
}

// FetchMe carrega o estado atual canônico do servidor
func (c *LegacyClient) FetchMe(ctx context.Context) (*MeResponse, error) {
	var res MeResponse
	_, _, err := c.doRequest(ctx, http.MethodGet, "/api/me", nil, &res)
	if err != nil {
		return nil, err
	}
	if res.Token != "" {
		c.token = res.Token
	}
	return &res, nil
}

// PushSave envia o progresso do personagem para persistência na nuvem
func (c *LegacyClient) PushSave(ctx context.Context, env SaveEnvelope) (*SaveResponse, error) {
	var res SaveResponse
	status, _, err := c.doRequest(ctx, http.MethodPost, "/api/save", env, &res)
	if err != nil {
		if status == 409 && (res.Version > 0 || res.ClientVersion > 0) {
			// Servidor retornou a versão atualizada para reconciliação otimista imediata
			return &res, nil
		}
		return nil, err
	}
	return &res, nil
}

// ClaimDailyReward resgata o brinde diário
func (c *LegacyClient) ClaimDailyReward(ctx context.Context) (map[string]any, error) {
	var res map[string]any
	_, _, err := c.doRequest(ctx, http.MethodPost, "/api/daily-reward/claim", map[string]any{}, &res)
	return res, err
}

// StashAll deposita o excesso de itens no depósito
func (c *LegacyClient) StashAll(ctx context.Context) (map[string]any, error) {
	var res map[string]any
	_, _, err := c.doRequest(ctx, http.MethodPost, "/api/depot/stash-all", map[string]any{}, &res)
	return res, err
}

// SellAll vende os itens de loot para obter moedas
func (c *LegacyClient) SellAll(ctx context.Context, itemIDs []string) (map[string]any, error) {
	if len(itemIDs) == 0 {
		return nil, nil
	}
	var res map[string]any
	_, _, err := c.doRequest(ctx, http.MethodPost, "/api/shop/sell-all", map[string]any{"items": itemIDs}, &res)
	return res, err
}
