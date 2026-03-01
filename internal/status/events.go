package status

import (
	"strconv"

	"github.com/thisilike/ts-status/internal/connection"
)

// HandleEvent processes a single RawMessage and returns true if state changed.
func (s *AppState) HandleEvent(msg connection.RawMessage) bool {
	switch msg.Type {
	case "auth":
		return s.handleAuth(msg)
	case "connectStatusChanged":
		return s.handleConnectStatusChanged(msg)
	case "clientSelfPropertyUpdated":
		return s.handleClientSelfPropertyUpdated(msg)
	case "talkStatusChanged":
		return s.handleTalkStatusChanged(msg)
	case "clientMoved":
		return s.handleClientMoved(msg)
	case "channels":
		return s.handleChannels(msg)
	case "channelPropertiesUpdated":
		return s.handleChannelPropertiesUpdated(msg)
	case "serverPropertiesUpdated":
		return s.handleServerPropertiesUpdated(msg)
	case "clientPropertiesUpdated":
		return s.handleClientPropertiesUpdated(msg)
	}
	return false
}

func (s *AppState) handleAuth(msg connection.RawMessage) bool {
	if msg.Payload == nil {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	conns, ok := msg.Payload["connections"].([]interface{})
	if !ok {
		return false
	}

	for _, raw := range conns {
		connMap, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}

		connID := jsonInt(connMap["id"])

		// Extract server UID from properties
		var uid string
		if props, ok := connMap["properties"].(map[string]interface{}); ok {
			uid, _ = props["uniqueIdentifier"].(string)
		}
		if uid == "" {
			continue
		}

		sc := s.getOrCreateByUID(uid)
		sc.ConnectionID = connID
		sc.Status = ConnectStatus(jsonInt(connMap["status"]))
		s.bindConn(connID, uid)

		// Server name from properties
		if props, ok := connMap["properties"].(map[string]interface{}); ok {
			if name, ok := props["name"].(string); ok {
				sc.ServerName = name
			}
		}

		// Our client ID
		clientID := jsonInt(connMap["clientId"])
		sc.ClientID = clientID

		// Build channel name map from channelInfos
		if channelInfos, ok := connMap["channelInfos"].(map[string]interface{}); ok {
			s.buildChannelMap(uid, channelInfos)
		}

		// Parse all clients from clientInfos
		sc.Clients = make(map[int]*Client)
		if clientInfos, ok := connMap["clientInfos"].([]interface{}); ok {
			for _, ci := range clientInfos {
				cMap, ok := ci.(map[string]interface{})
				if !ok {
					continue
				}
				cid := jsonInt(cMap["id"])
				chID := jsonString(cMap["channelId"])

				if cid == clientID {
					// Our own client
					sc.ChannelID = chID
					sc.ChannelName = s.resolveChannel(uid, chID)
					if props, ok := cMap["properties"].(map[string]interface{}); ok {
						applyClientProps(sc, props)
					}
				}

				// Track all clients (including self for completeness)
				cl := &Client{ID: cid, ChannelID: chID}
				if props, ok := cMap["properties"].(map[string]interface{}); ok {
					applyRemoteClientProps(cl, props)
				}
				sc.Clients[cid] = cl
			}
		}
	}

	return true
}

func (s *AppState) handleConnectStatusChanged(msg connection.RawMessage) bool {
	if msg.Payload == nil {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	connID := jsonInt(msg.Payload["connectionId"])

	// At status 2, info carries serverUid, serverName, clientId
	var uid string
	if info, ok := msg.Payload["info"].(map[string]interface{}); ok {
		uid, _ = info["serverUid"].(string)
	}

	var sc *ServerConnection
	if uid != "" {
		sc = s.getOrCreateByUID(uid)
		sc.ConnectionID = connID
		s.bindConn(connID, uid)
	} else {
		sc = s.byConnID(connID)
		if sc == nil {
			return false
		}
	}

	sc.Status = ConnectStatus(jsonInt(msg.Payload["status"]))

	if info, ok := msg.Payload["info"].(map[string]interface{}); ok {
		if clientID, ok := info["clientId"]; ok {
			sc.ClientID = jsonInt(clientID)
		}
		if name, ok := info["serverName"].(string); ok {
			sc.ServerName = name
		}
	}

	return true
}

func (s *AppState) handleClientSelfPropertyUpdated(msg connection.RawMessage) bool {
	if msg.Payload == nil {
		return false
	}

	flag, _ := msg.Payload["flag"].(string)
	if flag == "" {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	connID := jsonInt(msg.Payload["connectionId"])
	sc := s.byConnID(connID)
	if sc == nil {
		return false
	}

	switch flag {
	case "inputMuted":
		sc.InputMuted = jsonBool(msg.Payload["newValue"])
	case "outputMuted":
		sc.OutputMuted = jsonBool(msg.Payload["newValue"])
	case "away":
		sc.Away = jsonBool(msg.Payload["newValue"])
	case "awayMessage":
		if v, ok := msg.Payload["newValue"].(string); ok {
			sc.AwayMessage = v
		}
	case "nickname":
		if v, ok := msg.Payload["newValue"].(string); ok {
			sc.Nickname = v
		}
	case "flagTalking":
		sc.Talking = jsonBool(msg.Payload["newValue"])
	case "inputDeactivated":
		sc.InputDeactivated = jsonBool(msg.Payload["newValue"])
	case "talkPower":
		sc.TalkPower = jsonInt(msg.Payload["newValue"])
	default:
		return false
	}

	// Mirror change to self Client entry in Clients map
	if cl, ok := sc.Clients[sc.ClientID]; ok {
		switch flag {
		case "inputMuted":
			cl.InputMuted = sc.InputMuted
		case "outputMuted":
			cl.OutputMuted = sc.OutputMuted
		case "away":
			cl.Away = sc.Away
		case "awayMessage":
			cl.AwayMessage = sc.AwayMessage
		case "nickname":
			cl.Nickname = sc.Nickname
		case "flagTalking":
			cl.Talking = sc.Talking
		case "inputDeactivated":
			cl.InputDeactivated = sc.InputDeactivated
		case "talkPower":
			cl.TalkPower = sc.TalkPower
		}
	}

	return true
}

func (s *AppState) handleTalkStatusChanged(msg connection.RawMessage) bool {
	if msg.Payload == nil {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	connID := jsonInt(msg.Payload["connectionId"])
	sc := s.byConnID(connID)
	if sc == nil {
		return false
	}

	clientID := jsonInt(msg.Payload["clientId"])
	talking := jsonInt(msg.Payload["status"]) == 1

	if clientID == sc.ClientID {
		sc.Talking = talking
	}

	if sc.Clients != nil {
		if cl, ok := sc.Clients[clientID]; ok {
			cl.Talking = talking
		}
	}

	return true
}

func (s *AppState) handleClientMoved(msg connection.RawMessage) bool {
	if msg.Payload == nil {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	connID := jsonInt(msg.Payload["connectionId"])
	sc := s.byConnID(connID)
	if sc == nil {
		return false
	}

	clientID := jsonInt(msg.Payload["clientId"])
	newChID := jsonString(msg.Payload["newChannelId"])

	// Track our own channel
	if clientID == sc.ClientID {
		if newChID != "" {
			sc.ChannelID = newChID
			sc.ChannelName = s.resolveChannel(sc.ServerUID, newChID)
		}
	}

	// Update the client in our map
	if sc.Clients != nil {
		cl, ok := sc.Clients[clientID]
		if !ok {
			// New client we haven't seen before
			cl = &Client{ID: clientID}
			sc.Clients[clientID] = cl
		}
		if newChID != "" {
			cl.ChannelID = newChID
		}
		// Apply properties if present (sent when a client enters a subscribed channel)
		if props, ok := msg.Payload["properties"].(map[string]interface{}); ok {
			applyRemoteClientProps(cl, props)
		}
	}

	// Check if client left the server (moved to channel "0" or empty)
	if newChID == "0" || newChID == "" {
		delete(sc.Clients, clientID)
	}

	return true
}

func (s *AppState) handleChannels(msg connection.RawMessage) bool {
	if msg.Payload == nil {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	connID := jsonInt(msg.Payload["connectionId"])
	sc := s.byConnID(connID)
	if sc == nil {
		return false
	}

	if info, ok := msg.Payload["info"].(map[string]interface{}); ok {
		s.buildChannelMap(sc.ServerUID, info)
	}

	// Re-resolve channel name
	if sc.ChannelID != "" {
		sc.ChannelName = s.resolveChannel(sc.ServerUID, sc.ChannelID)
	}

	return true
}

func (s *AppState) handleChannelPropertiesUpdated(msg connection.RawMessage) bool {
	if msg.Payload == nil {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	connID := jsonInt(msg.Payload["connectionId"])
	sc := s.byConnID(connID)
	if sc == nil {
		return false
	}

	channelID := jsonString(msg.Payload["channelId"])
	if channelID == "" {
		return false
	}

	if props, ok := msg.Payload["properties"].(map[string]interface{}); ok {
		if name, ok := props["name"].(string); ok {
			uid := sc.ServerUID
			if s.Channels[uid] == nil {
				s.Channels[uid] = make(map[string]string)
			}
			s.Channels[uid][channelID] = name

			if sc.ChannelID == channelID {
				sc.ChannelName = name
				return true
			}
		}
	}
	return false
}

func (s *AppState) handleServerPropertiesUpdated(msg connection.RawMessage) bool {
	if msg.Payload == nil {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	connID := jsonInt(msg.Payload["connectionId"])
	sc := s.byConnID(connID)
	if sc == nil {
		return false
	}

	if props, ok := msg.Payload["properties"].(map[string]interface{}); ok {
		if name, ok := props["name"].(string); ok {
			sc.ServerName = name
			return true
		}
	}
	return false
}

// buildChannelMap extracts channel names from channelInfos. Caller must hold mu.
func (s *AppState) buildChannelMap(uid string, channelInfos map[string]interface{}) {
	chMap := make(map[string]string)

	var walk func(channels []interface{})
	walk = func(channels []interface{}) {
		for _, raw := range channels {
			ch, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			id := jsonString(ch["id"])
			if props, ok := ch["properties"].(map[string]interface{}); ok {
				if name, ok := props["name"].(string); ok && id != "" {
					chMap[id] = name
				}
			}
			if sub, ok := ch["subChannels"].([]interface{}); ok {
				walk(sub)
			}
		}
	}

	if roots, ok := channelInfos["rootChannels"].([]interface{}); ok {
		walk(roots)
	}

	s.Channels[uid] = chMap
}

func (s *AppState) handleClientPropertiesUpdated(msg connection.RawMessage) bool {
	if msg.Payload == nil {
		return false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	connID := jsonInt(msg.Payload["connectionId"])
	sc := s.byConnID(connID)
	if sc == nil {
		return false
	}

	clientID := jsonInt(msg.Payload["clientId"])
	if sc.Clients == nil {
		return false
	}

	cl, ok := sc.Clients[clientID]
	if !ok {
		return false
	}

	if props, ok := msg.Payload["properties"].(map[string]interface{}); ok {
		applyRemoteClientProps(cl, props)
		return true
	}
	return false
}

// applyClientProps sets ServerConnection fields from a client properties map.
func applyClientProps(sc *ServerConnection, props map[string]interface{}) {
	if v, ok := props["nickname"].(string); ok {
		sc.Nickname = v
	}
	sc.InputMuted = jsonBool(props["inputMuted"])
	sc.OutputMuted = jsonBool(props["outputMuted"])
	sc.InputDeactivated = jsonBool(props["inputDeactivated"])
	sc.Talking = jsonBool(props["flagTalking"])
	sc.Away = jsonBool(props["away"])
	if v, ok := props["awayMessage"].(string); ok {
		sc.AwayMessage = v
	}
	if v, ok := props["talkPower"]; ok {
		sc.TalkPower = jsonInt(v)
	}
}

// applyRemoteClientProps sets Client fields from a properties map.
func applyRemoteClientProps(cl *Client, props map[string]interface{}) {
	if v, ok := props["nickname"].(string); ok {
		cl.Nickname = v
	}
	if v, ok := props["inputMuted"]; ok {
		cl.InputMuted = jsonBool(v)
	}
	if v, ok := props["outputMuted"]; ok {
		cl.OutputMuted = jsonBool(v)
	}
	if v, ok := props["inputDeactivated"]; ok {
		cl.InputDeactivated = jsonBool(v)
	}
	if v, ok := props["flagTalking"]; ok {
		cl.Talking = jsonBool(v)
	}
	if v, ok := props["away"]; ok {
		cl.Away = jsonBool(v)
	}
	if v, ok := props["awayMessage"].(string); ok {
		cl.AwayMessage = v
	}
	if v, ok := props["talkPower"]; ok {
		cl.TalkPower = jsonInt(v)
	}
}

// jsonInt extracts an int from a JSON number (float64).
func jsonInt(v interface{}) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return 0
}

// jsonBool extracts a bool from a JSON value.
func jsonBool(v interface{}) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

// jsonString extracts a string from a JSON value, converting numbers if needed.
func jsonString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	if f, ok := v.(float64); ok {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return ""
}
