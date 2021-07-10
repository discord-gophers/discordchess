package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/DiscordGophers/discordchess"
	"github.com/bwmarrin/discordgo"
	_ "github.com/joho/godotenv/autoload"
)

func main() {
	dg, err := discordgo.New("Bot " + os.Getenv("DISCORD_API_KEY"))
	if err != nil {
		log.Fatalf("Failed to create discord session: %v", err)
	}
	defer dg.Close()

	prefix := os.Getenv("CMD_PREFIX")
	if prefix == "" {
		prefix = "!"
	}
	roomMatch := os.Getenv("ROOM_MATCH")

	log.Println("Starting:")
	log.Printf("  prefix: %q", prefix)
	log.Printf("  rooms: %q", roomMatch)

	dc := discordchess.New(prefix, roomMatch)

	dg.AddHandler(dc.MessageCreateHandler)

	dg.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsGuildMessageReactions

	if err := dg.Open(); err != nil {
		log.Fatalf("Failed to open discord connection: %v", err)
	}

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

	<-sc
}
