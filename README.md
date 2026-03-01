# ts-status

NDJSON bridge for the TeamSpeak Remote Apps WebSocket API.

Connects to TeamSpeak's local WebSocket, tracks server/channel/client state, and emits newline-delimited JSON on stdout whenever state changes. Designed to be consumed by status bar widgets, scripts, or any tool that can read NDJSON.

## Install

**AUR:**

```
yay -S ts-status
```

**go install:**

```
go install github.com/thisilike/ts-status@latest
```

**Manual:**

```
git clone https://github.com/thisilike/ts-status.git
cd ts-status
go build -o ts-status .
```

## Usage

```
ts-status [--addr ws://localhost:5899] [--apikey-path path]
```

| Flag | Default | Description |
|---|---|---|
| `--addr` | `ws://localhost:5899` | TeamSpeak Remote Apps WebSocket address |
| `--apikey-path` | `data/status_apikey.txt` | Path to persist the API key |

## Output

Each line is a JSON object. State messages contain the full server list:

```json
{"type":"state","servers":[{"serverUid":"abc=","serverName":"My Server","status":4,"statusText":"Connected to server","channelName":"Lobby","nickname":"user","inputMuted":false,"outputMuted":false,"inputDeactivated":false,"talking":false,"away":false,"awayMessage":"","talkPower":0,"channelMembers":[{"id":1,"isSelf":true,"nickname":"user","inputMuted":false,"outputMuted":false,"inputDeactivated":false,"talking":false,"away":false,"talkPower":0}]}]}
```

Error messages:

```json
{"type":"error","message":"connection failed: ..."}
```

## First run

On first connection, TeamSpeak will show an approval prompt for the remote app. After approval, the API key is automatically persisted to the path specified by `--apikey-path`.

## Related

- [dms-plugin-teamspeak](https://github.com/thisilike/dms-plugin-teamspeak) — DMS bar widget that consumes ts-status output
