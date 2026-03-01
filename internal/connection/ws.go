package connection

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/gorilla/websocket"
)

// RawMessage is the universal envelope for all TS5 WebSocket messages.
type RawMessage struct {
	Type    string                 `json:"type"`
	Payload map[string]interface{} `json:"payload,omitempty"`
	Status  map[string]interface{} `json:"status,omitempty"`
}

// MessageHandler is called for each received message.
type MessageHandler func(RawMessage)

// Client manages the WebSocket connection to the TS5 Remote Apps API.
type Client struct {
	conn      *websocket.Conn
	onMessage MessageHandler
	mu        sync.Mutex
}

// NewClient dials the WebSocket at addr and returns a Client.
func NewClient(addr string, onMessage MessageHandler) (*Client, error) {
	conn, _, err := websocket.DefaultDialer.Dial(addr, nil)
	if err != nil {
		return nil, fmt.Errorf("websocket dial: %w", err)
	}
	return &Client{conn: conn, onMessage: onMessage}, nil
}

// AuthParams configures the app identity sent during authentication.
type AuthParams struct {
	Identifier  string
	Version     string
	Name        string
	Description string
}

// SendAuth sends the authentication payload using the default sniffer identity.
func (c *Client) SendAuth(apiKey string) error {
	return c.SendAuthWithParams(apiKey, AuthParams{
		Identifier:  "net.thisilike.ts5sniffer",
		Version:     "1.0.0",
		Name:        "TS5 Event Sniffer",
		Description: "Captures and catalogs all TS5 Remote Apps events",
	})
}

// SendAuthWithParams sends the authentication payload with a custom app identity.
func (c *Client) SendAuthWithParams(apiKey string, params AuthParams) error {
	msg := map[string]interface{}{
		"type": "auth",
		"payload": map[string]interface{}{
			"identifier":  params.Identifier,
			"version":     params.Version,
			"name":        params.Name,
			"description": params.Description,
			"content": map[string]interface{}{
				"apiKey": apiKey,
			},
		},
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteJSON(msg)
}

// ReadLoop blocks reading messages until ctx is cancelled or an error occurs.
func (c *Client) ReadLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("read: %w", err)
		}

		var msg RawMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		c.onMessage(msg)
	}
}

// Close performs a clean WebSocket close.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.Close()
}
