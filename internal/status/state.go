package status

import (
	"sort"
	"strings"
	"sync"
)

// ConnectStatus represents the connection state to a TS5 server.
type ConnectStatus int

const (
	StatusDisconnected ConnectStatus = iota
	StatusConnecting
	StatusConnected
	StatusEstablishing
	StatusEstablished
)

func (s ConnectStatus) String() string {
	switch s {
	case StatusDisconnected:
		return "Not connected"
	case StatusConnecting:
		return "Connecting to server"
	case StatusConnected:
		return "Connected to server"
	case StatusEstablishing:
		return "Connecting to server"
	case StatusEstablished:
		return "Connected to server"
	default:
		return "Unknown"
	}
}

// Client represents another user visible on the server.
type Client struct {
	ID               int
	Nickname         string
	ChannelID        string
	InputMuted       bool
	OutputMuted      bool
	InputDeactivated bool
	Talking          bool
	Away             bool
	AwayMessage      string
	TalkPower        int
}

// ServerConnection holds the live state for one server.
type ServerConnection struct {
	ServerUID        string
	ConnectionID     int
	ServerName       string
	ClientID         int
	Status           ConnectStatus
	ChannelID        string
	ChannelName      string
	InputMuted       bool
	OutputMuted      bool
	InputDeactivated bool
	Talking          bool
	Away             bool
	AwayMessage      string
	Nickname         string
	TalkPower        int
	Clients          map[int]*Client // all visible clients keyed by client ID
}

// AppState is the thread-safe aggregate of all server connections.
type AppState struct {
	mu        sync.RWMutex
	Servers   map[string]*ServerConnection // keyed by server uniqueIdentifier
	Channels  map[string]map[string]string // serverUID → channelId → name
	connIndex map[int]string               // connectionId → serverUID
}

// NewAppState creates an empty AppState.
func NewAppState() *AppState {
	return &AppState{
		Servers:   make(map[string]*ServerConnection),
		Channels:  make(map[string]map[string]string),
		connIndex: make(map[int]string),
	}
}

// Reset clears all state so a fresh auth response can rebuild it.
func (s *AppState) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Servers = make(map[string]*ServerConnection)
	s.Channels = make(map[string]map[string]string)
	s.connIndex = make(map[int]string)
}

// Snapshot returns a copy of all connections safe for reading outside the lock.
func (s *AppState) Snapshot() []*ServerConnection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*ServerConnection, 0, len(s.Servers))
	for _, c := range s.Servers {
		cp := *c
		if c.Clients != nil {
			cp.Clients = make(map[int]*Client, len(c.Clients))
			for id, cl := range c.Clients {
				clCopy := *cl
				cp.Clients[id] = &clCopy
			}
		}
		out = append(out, &cp)
	}
	return out
}

// getOrCreateByUID returns the server entry for a UID, creating if needed. Caller must hold mu.
func (s *AppState) getOrCreateByUID(uid string) *ServerConnection {
	if sc, ok := s.Servers[uid]; ok {
		return sc
	}
	sc := &ServerConnection{ServerUID: uid}
	s.Servers[uid] = sc
	return sc
}

// byConnID looks up a server by its current connectionId. Caller must hold mu.
func (s *AppState) byConnID(connID int) *ServerConnection {
	if uid, ok := s.connIndex[connID]; ok {
		return s.Servers[uid]
	}
	return nil
}

// bindConn associates a connectionId with a server UID, cleaning up stale mappings. Caller must hold mu.
func (s *AppState) bindConn(connID int, uid string) {
	// Remove any old connIndex entry pointing to this UID
	for oldConn, oldUID := range s.connIndex {
		if oldUID == uid && oldConn != connID {
			delete(s.connIndex, oldConn)
		}
	}
	s.connIndex[connID] = uid
}

// ChannelMembers returns clients in the same channel as the local user, excluding self.
// Sorted by talk power descending, then nickname ascending.
func (sc *ServerConnection) ChannelMembers() []*Client {
	if sc.Clients == nil || sc.ChannelID == "" {
		return nil
	}
	var members []*Client
	for _, cl := range sc.Clients {
		if cl.ChannelID == sc.ChannelID {
			members = append(members, cl)
		}
	}
	sort.Slice(members, func(i, j int) bool {
		if members[i].TalkPower != members[j].TalkPower {
			return members[i].TalkPower > members[j].TalkPower
		}
		return strings.ToLower(members[i].Nickname) < strings.ToLower(members[j].Nickname)
	})
	return members
}

// resolveChannel looks up a channel name from the cache. Caller must hold mu.
func (s *AppState) resolveChannel(uid string, channelID string) string {
	if chans, ok := s.Channels[uid]; ok {
		if name, ok := chans[channelID]; ok {
			return name
		}
	}
	return channelID
}
