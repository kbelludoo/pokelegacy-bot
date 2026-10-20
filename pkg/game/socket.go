package game

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// SocketClient gerencia a conexão WebSocket / Socket.IO v4 com o PokeLegacy
// Isso garante o isolamento de instâncias e desconecta abas ativas do navegador (Pilar 4)
type SocketClient struct {
	baseURL       string
	token         string
	deviceID      string
	bindIP        string
	proxyURL      string
	clientRelease string

	conn       *websocket.Conn
	mu         sync.Mutex
	isClosed   bool
	onReplaced func(reason string)

	handlers   map[string][]func(payload any)
	handlersMu sync.RWMutex

	logPrefix string
}

func NewSocketClient(baseURL, token, deviceID, bindIP, proxyURL, accID string, onReplaced func(string)) *SocketClient {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &SocketClient{
		baseURL:       baseURL,
		token:         token,
		deviceID:      deviceID,
		bindIP:        bindIP,
		proxyURL:      proxyURL,
		clientRelease: DefaultClientRelease,
		onReplaced:    onReplaced,
		handlers:      make(map[string][]func(payload any)),
		logPrefix:     fmt.Sprintf("[%s][WS]", accID),
	}
}

// SetToken atualiza o token da conexão WebSocket
func (sc *SocketClient) SetToken(token string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.token = token
}

// StartLoop mantém a conexão Socket.IO ativa em segundo plano com reconexão automática
func (sc *SocketClient) StartLoop(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				sc.Close()
				return
			default:
			}

			if err := sc.connectAndListen(ctx); err != nil {
				select {
				case <-ctx.Done():
					return
				case <-time.After(5 * time.Second):
					// Tenta reconectar a cada 5 segundos
				}
			}
		}
	}()
}

func (sc *SocketClient) connectAndListen(ctx context.Context) error {
	wsScheme := "wss"
	cleanURL := strings.TrimPrefix(sc.baseURL, "https://")
	cleanURL = strings.TrimPrefix(cleanURL, "http://")
	if strings.HasPrefix(sc.baseURL, "http://") {
		wsScheme = "ws"
	}

	wsURL := fmt.Sprintf("%s://%s/socket.io/?EIO=4&transport=websocket", wsScheme, cleanURL)

	dialer := &websocket.Dialer{
		HandshakeTimeout: 12 * time.Second,
	}

	if sc.proxyURL != "" {
		if pURL, err := url.Parse(sc.proxyURL); err == nil {
			dialer.Proxy = http.ProxyURL(pURL)
		}
	}

	if sc.bindIP != "" {
		localIP := net.ParseIP(sc.bindIP)
		if localIP != nil {
			dialer.NetDialContext = (&net.Dialer{
				LocalAddr: &net.TCPAddr{IP: localIP},
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext
		}
	}

	headers := http.Header{}
	headers.Set("User-Agent", DefaultUserAgent)
	headers.Set("Origin", sc.baseURL)
	headers.Set("X-Poke-Aparelho", sc.deviceID)

	conn, resp, err := dialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("falha no handshake websocket (HTTP %d): %w", resp.StatusCode, err)
		}
		return fmt.Errorf("falha ao conectar websocket: %w", err)
	}

	sc.mu.Lock()
	sc.conn = conn
	sc.isClosed = false
	sc.mu.Unlock()

	defer func() {
		sc.mu.Lock()
		if sc.conn != nil {
			_ = sc.conn.Close()
			sc.conn = nil
		}
		sc.mu.Unlock()
	}()

	// Loop de leitura de pacotes Socket.IO v4
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_ = conn.SetReadDeadline(time.Now().Add(45 * time.Second))
		msgType, msgBytes, err := conn.ReadMessage()
		if err != nil {
			return err
		}

		if msgType != websocket.TextMessage {
			continue
		}

		msg := string(msgBytes)

		// 1. Handshake inicial do Engine.IO: '0{"sid":...}'
		if strings.HasPrefix(msg, "0") {
			// Envia autenticação do Socket.IO: '40{"token":"...","clientRelease":"...","aparelho":"..."}'
			authPayload := map[string]string{
				"token":         sc.token,
				"clientRelease": sc.clientRelease,
				"aparelho":      sc.deviceID,
			}
			payloadBytes, _ := json.Marshal(authPayload)
			packet := "40" + string(payloadBytes)

			sc.mu.Lock()
			_ = conn.WriteMessage(websocket.TextMessage, []byte(packet))
			sc.mu.Unlock()
			continue
		}

		// 2. Ping do Engine.IO: '2' -> Responder Pong '3'
		if msg == "2" {
			sc.mu.Lock()
			_ = conn.WriteMessage(websocket.TextMessage, []byte("3"))
			sc.mu.Unlock()
			continue
		}

		// 3. Autenticado com sucesso no Socket.IO: '40{"sid":...}'
		if strings.HasPrefix(msg, "40") {

			// Emite hunt:resume com ackId 0 imediatamente para computar o progresso oficial
			sc.mu.Lock()
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`420["hunt:resume",{"depotDelta":1}]`))
			sc.mu.Unlock()
			continue
		}

		// 3.5. Respostas de confirmação (Acks) do Socket.IO: '43<ackId>[payload]'
		if strings.HasPrefix(msg, "43") {
			idx := strings.Index(msg, "[")
			if idx != -1 {
				var arr []any
				if err := json.Unmarshal([]byte(msg[idx:]), &arr); err == nil && len(arr) >= 1 {
					if resMap, ok := arr[0].(map[string]any); ok {
						if okVal, _ := resMap["ok"].(bool); okVal {
							if off, ok := resMap["official"].(map[string]any); ok {
								var v int
								if vAny, exists := resMap["version"]; exists {
									switch n := vAny.(type) {
									case float64:
										v = int(n)
									case int:
										v = n
									}
								}
								sc.dispatch("hunt:resumeResponse", map[string]any{
									"official": off,
									"version":  v,
								})
							}
						}
					}
				}
			}
			continue
		}

		// 4. Mensagens e Eventos do Socket.IO: '42["eventName", payload]'
		if strings.HasPrefix(msg, "42") {
			var arr []any
			if err := json.Unmarshal([]byte(msg[2:]), &arr); err == nil && len(arr) >= 1 {
				if eventName, ok := arr[0].(string); ok {
					var payload any
					if len(arr) >= 2 {
						payload = arr[1]
					}
					// Auto-accept convite de party
					if eventName == "party:invited" {
						if m, ok := payload.(map[string]any); ok {
							if pID, ok := m["partyId"].(string); ok && pID != "" {
								log.Printf("%s Convite de party recebido (Party %s de %v). Aceitando automaticamente...", sc.logPrefix, pID, m["fromName"])
								_ = sc.Emit("party:accept", map[string]any{"partyId": pID})
							}
						}
					}
					sc.dispatch(eventName, payload)
				}
			}
			continue
		}

		// 5. Detecção de expulsão de sessão: 'session:replaced' ou 'outraAba'
		if strings.Contains(msg, "session:replaced") || strings.Contains(msg, "outraAba") {
			log.Printf("%s 🚨 Aviso recebido: sessão substituída por outro cliente.", sc.logPrefix)
			if sc.onReplaced != nil {
				sc.onReplaced("session:replaced")
			}
		}
	}
}

func (sc *SocketClient) dispatch(event string, payload any) {
	sc.handlersMu.RLock()
	handlers := sc.handlers[event]
	sc.handlersMu.RUnlock()

	for _, h := range handlers {
		go h(payload)
	}
}

// On registra um handler para eventos recebidos via Socket.IO
func (sc *SocketClient) On(event string, handler func(payload any)) {
	sc.handlersMu.Lock()
	defer sc.handlersMu.Unlock()
	sc.handlers[event] = append(sc.handlers[event], handler)
}

// Emit envia um evento para o servidor Socket.IO
func (sc *SocketClient) Emit(event string, payload any) error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.conn == nil || sc.isClosed {
		return fmt.Errorf("websocket não conectado")
	}

	var data []byte
	var err error
	if payload == nil {
		data, err = json.Marshal([]any{event})
	} else {
		data, err = json.Marshal([]any{event, payload})
	}
	if err != nil {
		return err
	}

	packet := "42" + string(data)
	return sc.conn.WriteMessage(websocket.TextMessage, []byte(packet))
}

// ResumeHunt solicita ao servidor a consolidação e retorno do progresso oficial da caçada
func (sc *SocketClient) ResumeHunt() error {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.conn == nil || sc.isClosed {
		return fmt.Errorf("websocket não conectado")
	}
	return sc.conn.WriteMessage(websocket.TextMessage, []byte(`420["hunt:resume",{"depotDelta":1}]`))
}

func (sc *SocketClient) Close() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.isClosed = true
	if sc.conn != nil {
		_ = sc.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bot shutdown"))
		_ = sc.conn.Close()
		sc.conn = nil
	}
}
