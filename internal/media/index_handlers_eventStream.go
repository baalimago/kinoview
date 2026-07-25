package media

import (
	"encoding/json"
	"io"
	"time"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/model"
	"golang.org/x/net/websocket"
)

const (
	defaultHeartbeatInterval = 5 * time.Second
	defaultPongTimeout       = 1 * time.Second
	defaultPingWriteTimeout  = 1 * time.Second
	defaultPongGrace         = 10 * time.Second
)

func (i *Indexer) handleWebsocketConnection(ws *websocket.Conn) {
	defer ws.Close()

	// Trigger a suggestions cascade on connect so the user sees fresh
	// suggestions on arrival instead of one-session-behind.
	i.handleConnect()

	// Subscribe to suggestions broadcasts. The cascade triggered above
	// (or an existing one) will push results through this channel.
	suggestionsCh := i.subscribeSuggestions()
	defer i.unsubscribeSuggestions(suggestionsCh)

	pongChan := make(chan struct{})
	// Buffer errChan to update state if socket dies
	errChan := make(chan error, 1)

	go i.readLoop(ws, pongChan, errChan)
	go i.broadcastToClient(ws, suggestionsCh, errChan)
	i.heartbeatLoop(ws, pongChan, errChan)
}

// broadcastToClient listens for server→client events and writes them to the
// websocket. Returns when the error channel signals or suggestions channel closes.
func (i *Indexer) broadcastToClient(ws *websocket.Conn, suggestionsCh <-chan model.SuggestionsPayload, errChan <-chan error) {
	for {
		select {
		case <-errChan:
			return
		case payload, ok := <-suggestionsCh:
			if !ok {
				return
			}
			event := model.Event[model.SuggestionsPayload]{
				Type:    model.SuggestionsEvent,
				Created: time.Now(),
				Payload: payload,
			}
			i.wsWriteMu.Lock()
			err := websocket.JSON.Send(ws, event)
			i.wsWriteMu.Unlock()
			if err != nil {
				ancli.Warnf("broadcastToClient: failed to send suggestions event: %v", err)
				return
			}
		}
	}
}

func (i *Indexer) readLoop(ws *websocket.Conn, pongChan chan<- struct{}, errChan chan<- error) {
	for {
		var rawEvent struct {
			Type    model.EventType `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := websocket.JSON.Receive(ws, &rawEvent); err != nil {
			if err != io.EOF {
				ancli.Warnf("websocket receive error: %v", err)
			}
			errChan <- err
			return
		}

		i.handleIncomingEvent(rawEvent.Type, rawEvent.Payload, pongChan)
	}
}

func (i *Indexer) handleIncomingEvent(eventType model.EventType, payload json.RawMessage, pongChan chan<- struct{}) {
	switch eventType {
	case model.HealthEvent:
		select {
		case pongChan <- struct{}{}:
		default:
		}
	case model.ClientContextEvent:
		var userCtx model.ClientContext
		if err := json.Unmarshal(payload, &userCtx); err != nil {
			ancli.Warnf("failed to unmarshal context: %v", err)
			return
		}
		if i.clientContextMgr == nil {
			ancli.Warnf("user context manager not set; dropping client context")
			return
		}
		if err := i.clientContextMgr.StoreClientContext(userCtx); err != nil {
			ancli.Warnf("failed to store client context: %v", err)
			return
		}
		ancli.Okf("stored client context")
	}
}

// heartbeatLoop periodically sends a health ping and expects a pong response.
//
// Timings are intentionally configurable (via Indexer fields) to make this
// routine testable without slow sleeps.
func (i *Indexer) heartbeatLoop(ws *websocket.Conn, pongChan <-chan struct{}, errChan <-chan error) {
	interval := i.heartbeatInterval
	if interval <= 0 {
		interval = defaultHeartbeatInterval
	}
	pongTimeout := i.pongTimeout
	if pongTimeout <= 0 {
		pongTimeout = defaultPongTimeout
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-errChan:
			i.handleDisconnect(reasonSocketError)
			return
		case <-ticker.C:
			if err := i.sendHealthPing(ws); err != nil {
				ancli.Warnf("failed to send health ping: %v", err)
				i.handleDisconnect(reasonPingFailed)
				return
			}

			if !i.waitForPong(pongChan, errChan, pongTimeout) {
				ancli.Warnf("client health check timed out")
				// Grace period for pong timeout: the client may have
				// been backgrounded. Wait and see if it recovers.
				if i.pongGrace > 0 {
					select {
					case <-pongChan:
						ancli.Noticef("client recovered during pong grace period")
						continue
					case <-errChan:
						i.handleDisconnect(reasonSocketError)
						return
					case <-time.After(i.pongGrace):
					}
				}
				i.handleDisconnect(reasonPongTimeout)
				return
			}
		}
	}
}

func (i *Indexer) sendHealthPing(ws *websocket.Conn) error {
	ping := model.Event[model.Health]{
		Type:    model.HealthEvent,
		Created: time.Now(),
		Payload: model.Health{},
	}

	writeTimeout := i.pingWriteTimeout
	if writeTimeout <= 0 {
		writeTimeout = defaultPingWriteTimeout
	}
	if err := ws.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return err
	}
	i.wsWriteMu.Lock()
	defer i.wsWriteMu.Unlock()
	return websocket.JSON.Send(ws, ping)
}

func (i *Indexer) waitForPong(pongChan <-chan struct{}, errChan <-chan error, timeout time.Duration) bool {
	if timeout <= 0 {
		timeout = defaultPongTimeout
	}
	select {
	case <-pongChan:
		return true
	case <-time.After(timeout):
		ancli.Warnf("client health check timed out")
		return false
	case <-errChan:
		return false
	}
}
