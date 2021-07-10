# discordchess

Simple straight forward discord chess bot using
https://github.com/notnil/chess.

## Usage

```bash
# With proper env variables
$ go run github.com/DiscordGophers/discordchess/cmd/discordchess
```

## Environment variables:

(automatically loads .env file)

| key             | value                                      |
| --------------- | ------------------------------------------ |
| DISCORD_API_KEY | discord bot token                          |
| CMD_PREFIX      | bot command prefix i.e: '!'                |
| ROOM_MATCH      | regexp to only allow in certain room names |

## Optionals

- `imagemagick` `convert` command to send board image (will send as text if
  unavailable)
- `stockfish` https://stockfishchess.org/download/linux/ for bot playing
