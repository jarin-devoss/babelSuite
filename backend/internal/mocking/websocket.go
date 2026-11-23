package mocking

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"

)

// webSocketUpgrader accepts any origin: the endpoint is only reachable with the
// mock token, which only a suite's own gateway holds, and a browser's Origin says
// nothing about which page asked the gateway.
var webSocketUpgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool { return true },
}

const webSocketWriteTimeout = 10 * time.Second

// serveWebSocket is the WebSocket mock: the gateway proxies an upgrade here (an
// upgrade cannot be accepted by a plugin), and the connection plays the matched
// example. On connect it sends the example's answer; a JSON array is sent as one
// frame per element. Every message the client sends is put to the engine like a
// request body, and the answer goes back the same way.
func (h *Handler) serveWebSocket(w http.ResponseWriter, r *http.Request) {
	suiteID, surfaceID, operationID := r.PathValue("suiteId"), r.PathValue("surfaceId"), r.PathValue("operationId")

	// Resolve once before upgrading, so an unknown suite, surface or operation is
	// an HTTP error the caller can read rather than a connection that opens and
	// closes.
	greeting, err := h.service.InvokeAdapter(r.Context(), suiteID, surfaceID, operationID, "websocket", r)
	if err != nil {
		h.writeLookupError(w, err)
		return
	}

	conn, err := webSocketUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade has already answered
	}
	defer conn.Close()

	if !sendFrames(conn, greeting) {
		return
	}
	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		request := r.Clone(r.Context())
		request.Method = http.MethodPost
		request.Body = io.NopCloser(bytes.NewReader(payload))
		request.ContentLength = int64(len(payload))
		if messageType == websocket.TextMessage {
			request.Header.Set("Content-Type", "application/json")
		}
		answer, err := h.service.InvokeAdapter(r.Context(), suiteID, surfaceID, operationID, "websocket", request)
		if err != nil {
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "the mock engine could not answer"), time.Now().Add(webSocketWriteTimeout))
			return
		}
		if !sendFrames(conn, answer) {
			return
		}
	}
}

// sendFrames writes an answer to the connection: a JSON array is one frame per
// element, anything else is one frame.
func sendFrames(conn *websocket.Conn, result *Result) bool {
	if result == nil {
		return true
	}
	body := bytes.TrimSpace(result.Body)
	if len(body) == 0 {
		return true
	}
	frames := [][]byte{body}
	if body[0] == '[' {
		var items []json.RawMessage
		if json.Unmarshal(body, &items) == nil {
			frames = frames[:0]
			for _, item := range items {
				frames = append(frames, unquoteFrame(item))
			}
		}
	}
	for _, frame := range frames {
		_ = conn.SetWriteDeadline(time.Now().Add(webSocketWriteTimeout))
		if err := conn.WriteMessage(websocket.TextMessage, frame); err != nil {
			return false
		}
	}
	return true
}

// unquoteFrame sends a JSON string element as its text, and any other element as
// its JSON.
func unquoteFrame(item json.RawMessage) []byte {
	var text string
	if len(item) > 0 && item[0] == '"' && json.Unmarshal(item, &text) == nil {
		return []byte(text)
	}
	return []byte(strings.TrimSpace(string(item)))
}
