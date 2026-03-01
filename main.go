package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/thisilike/ts-status/internal/connection"
	"github.com/thisilike/ts-status/internal/status"
	"github.com/thisilike/ts-status/internal/storage"
)

const (
	resyncInterval = 30 * time.Second
	reconnectDelay = 2 * time.Second
)

var authParams = connection.AuthParams{
	Identifier:  "net.thisilike.tsstatus",
	Version:     "1.0.0",
	Name:        "TS Status",
	Description: "Headless NDJSON status output for TeamSpeak",
}

type clientJSON struct {
	ID               int    `json:"id"`
	IsSelf           bool   `json:"isSelf,omitempty"`
	Nickname         string `json:"nickname"`
	InputMuted       bool   `json:"inputMuted"`
	OutputMuted      bool   `json:"outputMuted"`
	InputDeactivated bool   `json:"inputDeactivated"`
	Talking          bool   `json:"talking"`
	Away             bool   `json:"away"`
	AwayMessage      string `json:"awayMessage,omitempty"`
	TalkPower        int    `json:"talkPower"`
}

type serverJSON struct {
	ServerUID        string       `json:"serverUid"`
	ServerName       string       `json:"serverName"`
	Status           int          `json:"status"`
	StatusText       string       `json:"statusText"`
	ChannelName      string       `json:"channelName"`
	Nickname         string       `json:"nickname"`
	InputMuted       bool         `json:"inputMuted"`
	OutputMuted      bool         `json:"outputMuted"`
	InputDeactivated bool         `json:"inputDeactivated"`
	Talking          bool         `json:"talking"`
	Away             bool         `json:"away"`
	AwayMessage      string       `json:"awayMessage"`
	TalkPower        int          `json:"talkPower"`
	ChannelMembers   []clientJSON `json:"channelMembers"`
}

type stateMessage struct {
	Type    string       `json:"type"`
	Servers []serverJSON `json:"servers"`
}

type errorMessage struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func emitState(state *status.AppState) {
	snap := state.Snapshot()
	servers := make([]serverJSON, 0, len(snap))
	for _, sc := range snap {
		members := sc.ChannelMembers()
		memberList := make([]clientJSON, 0, len(members))
		for _, m := range members {
			memberList = append(memberList, clientJSON{
				ID:               m.ID,
				IsSelf:           m.ID == sc.ClientID,
				Nickname:         m.Nickname,
				InputMuted:       m.InputMuted,
				OutputMuted:      m.OutputMuted,
				InputDeactivated: m.InputDeactivated,
				Talking:          m.Talking,
				Away:             m.Away,
				AwayMessage:      m.AwayMessage,
				TalkPower:        m.TalkPower,
			})
		}

		servers = append(servers, serverJSON{
			ServerUID:        sc.ServerUID,
			ServerName:       sc.ServerName,
			Status:           int(sc.Status),
			StatusText:       sc.Status.String(),
			ChannelName:      sc.ChannelName,
			Nickname:         sc.Nickname,
			InputMuted:       sc.InputMuted,
			OutputMuted:      sc.OutputMuted,
			InputDeactivated: sc.InputDeactivated,
			Talking:          sc.Talking,
			Away:             sc.Away,
			AwayMessage:      sc.AwayMessage,
			TalkPower:        sc.TalkPower,
			ChannelMembers:   memberList,
		})
	}
	msg := stateMessage{Type: "state", Servers: servers}
	data, _ := json.Marshal(msg)
	fmt.Println(string(data))
}

func emitError(message string) {
	msg := errorMessage{Type: "error", Message: message}
	data, _ := json.Marshal(msg)
	fmt.Println(string(data))
}

func main() {
	addr := flag.String("addr", "ws://localhost:5899", "TeamSpeak Remote Apps WebSocket address")
	apiKeyPath := flag.String("apikey-path", "data/status_apikey.txt", "Path to persist the API key")
	flag.Parse()

	state := status.NewAppState()
	msgCh := make(chan connection.RawMessage, 64)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	// Connection manager
	go connManager(ctx, *addr, *apiKeyPath, msgCh)

	// Event loop: WS messages → state updates → NDJSON output
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgCh:
			if !ok {
				return
			}
			// Handle auth response: save API key and reset state for full resync
			if msg.Type == "auth" && msg.Status != nil {
				if code, ok := msg.Status["code"].(float64); ok && code == 0 {
					if msg.Payload != nil {
						if key, ok := msg.Payload["apiKey"].(string); ok && key != "" {
							_ = storage.SaveAPIKey(*apiKeyPath, key)
						}
					}
					state.Reset()
				}
			}

			if state.HandleEvent(msg) {
				emitState(state)
			}
		}
	}
}

func connManager(ctx context.Context, addr, apiKeyPath string, msgCh chan<- connection.RawMessage) {
	for {
		if ctx.Err() != nil {
			return
		}

		apiKey, _ := storage.LoadAPIKey(apiKeyPath)

		fmt.Fprintf(os.Stderr, "ts-status: connecting to %s\n", addr)
		client, err := connection.NewClient(addr, func(msg connection.RawMessage) {
			msgCh <- msg
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "ts-status: connection failed: %v\n", err)
			emitError(fmt.Sprintf("connection failed: %v", err))
			if !waitOrDone(ctx, reconnectDelay) {
				return
			}
			continue
		}

		if err := client.SendAuthWithParams(apiKey, authParams); err != nil {
			fmt.Fprintf(os.Stderr, "ts-status: auth send failed: %v\n", err)
			client.Close()
			if !waitOrDone(ctx, reconnectDelay) {
				return
			}
			continue
		}

		fmt.Fprintf(os.Stderr, "ts-status: connected\n")

		connCtx, connCancel := context.WithCancel(ctx)
		connErr := make(chan error, 1)
		go func() {
			connErr <- client.ReadLoop(connCtx)
		}()

		resync := time.NewTimer(resyncInterval)
		select {
		case err := <-connErr:
			if err != nil {
				fmt.Fprintf(os.Stderr, "ts-status: read error: %v\n", err)
			}
		case <-resync.C:
			fmt.Fprintf(os.Stderr, "ts-status: resync timer fired\n")
		case <-ctx.Done():
		}
		resync.Stop()
		connCancel()
		client.Close()

		if ctx.Err() != nil {
			return
		}

		waitOrDone(ctx, reconnectDelay)
	}
}

func waitOrDone(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}
