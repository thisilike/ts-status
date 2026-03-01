package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alecthomas/kong"
	"github.com/thisilike/ts-status/internal/connection"
	"github.com/thisilike/ts-status/internal/status"
	"github.com/thisilike/ts-status/internal/storage"
)

const version = "2.0.0"

const (
	resyncInterval = 30 * time.Second
	reconnectDelay = 2 * time.Second
)

var cli struct {
	Addr       string           `help:"TeamSpeak Remote Apps WebSocket address." default:"ws://localhost:5899"`
	ApiKeyPath string           `help:"Path to persist the API key." default:"data/status_apikey.txt" name:"apikey-path"`
	MaxFps     int              `help:"Max output frames per second. 0 = unlimited." default:"0" name:"max-fps"`
	Version    kong.VersionFlag `help:"Print version and exit." short:"v"`
}

var authParams = connection.AuthParams{
	Identifier:  "net.thisilike.tsstatus",
	Version:     version,
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

type outputMessage struct {
	Connected bool         `json:"connected"`
	Error     string       `json:"error"`
	Servers   []serverJSON `json:"servers"`
}

func buildServerList(state *status.AppState) []serverJSON {
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
	return servers
}

func emit(connected bool, errMsg string, state *status.AppState) {
	var servers []serverJSON
	if connected {
		servers = buildServerList(state)
	} else {
		servers = []serverJSON{}
	}
	msg := outputMessage{
		Connected: connected,
		Error:     errMsg,
		Servers:   servers,
	}
	data, _ := json.Marshal(msg)
	fmt.Println(string(data))
}

func main() {
	kong.Parse(&cli, kong.Vars{"version": "ts-status " + version})

	maxFps := cli.MaxFps
	if maxFps < 0 {
		maxFps = 0
	} else if maxFps > 60 {
		maxFps = 60
	}

	state := status.NewAppState()
	msgCh := make(chan connection.RawMessage, 64)
	connErrCh := make(chan string, 8)
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
	go connManager(ctx, cli.Addr, cli.ApiKeyPath, msgCh, connErrCh)

	// Emit initial disconnected state
	emit(false, "", state)

	connected := false
	dirty := false
	currentErr := ""

	if maxFps > 0 {
		// Throttled mode: emit at most maxFps times per second
		ticker := time.NewTicker(time.Second / time.Duration(maxFps))
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return

			case errMsg := <-connErrCh:
				connected = false
				currentErr = errMsg
				state.Reset()
				dirty = true

			case msg, ok := <-msgCh:
				if !ok {
					return
				}
				if msg.Type == "auth" && msg.Status != nil {
					if code, ok := msg.Status["code"].(float64); ok && code == 0 {
						if msg.Payload != nil {
							if key, ok := msg.Payload["apiKey"].(string); ok && key != "" {
								_ = storage.SaveAPIKey(cli.ApiKeyPath, key)
							}
						}
						state.Reset()
						connected = true
						currentErr = ""
					}
				}
				if state.HandleEvent(msg) {
					dirty = true
				}

			case <-ticker.C:
				if dirty {
					emit(connected, currentErr, state)
					dirty = false
				}
			}
		}
	} else {
		// Unlimited mode: emit immediately on every change
		for {
			select {
			case <-ctx.Done():
				return

			case errMsg := <-connErrCh:
				connected = false
				state.Reset()
				emit(false, errMsg, state)

			case msg, ok := <-msgCh:
				if !ok {
					return
				}
				if msg.Type == "auth" && msg.Status != nil {
					if code, ok := msg.Status["code"].(float64); ok && code == 0 {
						if msg.Payload != nil {
							if key, ok := msg.Payload["apiKey"].(string); ok && key != "" {
								_ = storage.SaveAPIKey(cli.ApiKeyPath, key)
							}
						}
						state.Reset()
						connected = true
					}
				}
				if state.HandleEvent(msg) {
					emit(connected, "", state)
				}
			}
		}
	}
}

func connManager(ctx context.Context, addr, apiKeyPath string, msgCh chan<- connection.RawMessage, errCh chan<- string) {
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
			errCh <- fmt.Sprintf("connection failed: %v", err)
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
