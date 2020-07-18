package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

	"github.com/bwmarrin/discordgo"
)

var meRe *regexp.Regexp = regexp.MustCompile("\\b((me|ME)\\s*?)(\\r|\\n|\\.|$)")

func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID || m.Author.Bot {
		return
	}

	if count := len(meRe.FindAllStringIndex(m.Content, -1)); count > 0 {
		var message strings.Builder
		message.Grow(count*4 + len("<@>") + len(m.Author.ID) + 1)
		fmt.Fprintf(&message, "<@%s> ", m.Author.ID)
		for i := 0; i < count; i++ {
			fmt.Fprintf(&message, "muh ")
		}

		s.ChannelMessageSend(m.ChannelID, message.String())
	}
}

func main() {
	token := strings.Replace(os.Getenv("DISCORD_AUTH"), "\"", "", -1)
	discord, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Printf("unable to create new discord instance")
		log.Fatal(err)
	}

	// Register the messageCreate func as a callback for MessageCreate events.
	discord.AddHandler(messageCreate)

	// Open a websocket connection to Discord and begin listening.
	err = discord.Open()
	if err != nil {
		fmt.Println("error opening connection,", err)
		return
	}

	// Wait here until CTRL-C or other term signal is received.
	fmt.Println("Bot is now running.  Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc

	// Cleanly close down the Discord session.
	discord.Close()

}
