package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/bwmarrin/discordgo"
	_ "github.com/joho/godotenv/autoload"
)

func main() {

	dg, err := discordgo.New("Bot " + os.Getenv("DISCORD_API_KEY"))
	if err != nil {
		log.Fatalf("Failed to create discord session: %v", err)
	}
	defer dg.Close()

	dg.AddHandler(messageCreateHandler)

	dg.Identify.Intents = discordgo.IntentsGuildMessages

	if err := dg.Open(); err != nil {
		log.Fatalf("Failed to open discord connection: %v", err)
	}

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)

	<-sc
}
