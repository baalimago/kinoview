package media

import (
	"encoding/json"
	"io"
	"time"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/kinoview/internal/model"
	"golang.org/x/net/websocket"
)

func (i *Indexer) handleWebsocketConnection(ws *websocket.Conn) {
	defer ws.Close()

	pongChan := make(chan struct{})
	// Buffer errChan to update state if socket dies
	errChan := make(chan error, 1)

	go i.readLoop(ws, pongChan, errChan)
	i.heartbeatLoop(ws, pongChan, errChan)
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
		var ctx model.ClientContext
		if err := json.Unmarshal(payload, &ctx); err != nil {
			ancli.Warnf("failed to unmarshal context: %v", err)
			return
		}
		i.clientCtxMu.Lock()
		i.lastClientContext = ctx
		i.clientCtxMu.Unlock()
		ancli.Okf("updated client context")
	}
}

func (i *Indexer) heartbeatLoop(ws *websocket.Conn, pongChan <-chan struct{}, errChan <-chan error) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-errChan:
			i.handleDisconnect()
			return
		case <-ticker.C:
			if err := i.sendHealthPing(ws); err != nil {
				ancli.Warnf("failed to send health ping: %v", err)
				i.handleDisconnect()
				return
			}

			if !i.waitForPong(pongChan, errChan) {
				i.handleDisconnect()
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
	if err := ws.SetWriteDeadline(time.Now().Add(1 * time.Second)); err != nil {
		return err
	}
	return websocket.JSON.Send(ws, ping)
}

func (i *Indexer) waitForPong(pongChan <-chan struct{}, errChan <-chan error) bool {
	select {
	case <-pongChan:
		return true
	case <-time.After(1 * time.Second):
		ancli.Warnf("client health check timed out")
		return false
	case <-errChan:
		return false
	}
}
