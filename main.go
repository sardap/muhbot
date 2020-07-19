package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"unicode"

	"github.com/bwmarrin/discordgo"
)

var meRe *regexp.Regexp = regexp.MustCompile("\\b(((me)\\.?(\\x60|\\*\\*__|\\*+|~~|__|\\|\\|)?)\\s*?)(\\r|\\n|\\.|$)")

func reverse(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

func isUpper(s string) bool {
	for _, r := range s {
		if !unicode.IsUpper(r) && unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID || m.Author.Bot {
		return
	}

	matches := meRe.FindAllStringSubmatchIndex(strings.ToLower(m.Content), -1)
	count := len(matches)
	if count > 0 {
		var message strings.Builder
		message.Grow(count*4 + len("<@>") + len(m.Author.ID) + 1)
		fmt.Fprintf(&message, "<@%s> ", m.Author.ID)
		for _, match := range matches {
			modify := ""
			if match[8] > 0 {
				modify = m.Content[match[8]:match[9]]
			}
			target := m.Content[match[6]:match[7]]
			muhStr := "muh"
			if isUpper(target) {
				muhStr = strings.ToUpper(muhStr)
			} else {
				if unicode.IsUpper(rune(target[0])) {
					muhStr = fmt.Sprintf(
						"%s%s",
						strings.ToUpper(string(muhStr[0])), muhStr[1:len(muhStr)],
					)
				}
				if unicode.IsUpper(rune(target[len(target)-1])) {
					muhStr = fmt.Sprintf(
						"%s%s",
						muhStr[0:len(muhStr)-1], strings.ToUpper(string(muhStr[len(muhStr)-1])),
					)
				}
			}

			fmt.Fprintf(&message, "%s%s%s ", modify, muhStr, reverse(modify))
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
