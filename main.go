package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/bwmarrin/discordgo"
	"github.com/go-redis/redis"
)

// NOTE commandRe is set in main
var (
	meRe      *regexp.Regexp
	commandRe *regexp.Regexp
	client    *redis.Client
)

func init() {
	meRe = regexp.MustCompile("()(((?P<me>me)(\\.|!|\\?|\"$|'$)?(?P<punc>\\x60|_+|\\*+|\\*+_+|~~|\\|\\|)?(\\.|!|\\?|\"$|'$))\\s*?)(\\r|\\n|\\.|$)")

	redisAddress := os.Getenv("REDIS_ADDRESS")
	dbNum, err := strconv.Atoi(os.Getenv("REDIS_DB"))
	if err != nil {
		panic(err)
	}
	password := os.Getenv("REDIS_PASSWORD")

	tries := 0
	for tries < 5 {
		client = redis.NewClient(&redis.Options{
			Addr:     redisAddress,
			Password: password,
			DB:       dbNum,
		})
		_, err := client.Ping().Result()
		if err == nil {
			break
		}
		time.Sleep(time.Duration(5) * time.Second)
	}
	if err != nil {
		panic(err)
	}
}

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

func userKey(gID, uID string) string {
	return fmt.Sprintf("%s:%s", gID, uID)
}

func logMuh(gID, uID string, n int) {
	client.IncrBy(userKey(gID, uID), int64(n))
}

func handleCommand(s *discordgo.Session, m *discordgo.MessageCreate) {
	if regexp.MustCompile(".*?help$").Match([]byte(m.Content)) {
		s.ChannelMessageSend(
			m.ChannelID,
			fmt.Sprintf(
				"<@%s> Commands:\n"+
					"\"<@%s> muh stats\" to show how many times you have said **the** word.",
				m.Author.ID, s.State.User.ID,
			),
		)
	} else if regexp.MustCompile(".*?muh.*?stats$").Match([]byte(m.Content)) {
		res := client.Get(userKey(m.GuildID, m.Author.ID))
		if res.Val() == "" {
			s.ChannelMessageSend(
				m.ChannelID,
				fmt.Sprintf(
					"<@%s> you have never **the** word since I started writing it down.",
					m.Author.ID,
				),
			)
			return
		}

		n, err := strconv.Atoi(res.Val())
		if err != nil {
			s.ChannelMessageSend(
				m.ChannelID,
				fmt.Sprintf("<@%s> Sever error tell paul.", m.Author.ID),
			)
			log.Printf("error getting data from DB %v\n", err)
			return
		}

		s.ChannelMessageSend(
			m.ChannelID,
			fmt.Sprintf(
				"<@%s> you have said **the** word %d times (since I started writing it down).",
				m.Author.ID, n,
			),
		)
	}
}

func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID || m.Author.Bot {
		return
	}

	matches := meRe.FindAllStringSubmatchIndex(strings.ToLower(m.Content), -1)
	if len(matches) > 0 {
		var message strings.Builder
		message.Grow(len(matches)*4 + len("<@>") + len(m.Author.ID) + 1)
		fmt.Fprintf(&message, "<@%s> ", m.Author.ID)
		for _, match := range matches {
			modify := ""
			if match[12] > 0 {
				modify = m.Content[match[12]:match[13]]
			}
			target := m.Content[match[8]:match[9]]
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

		go logMuh(m.GuildID, m.Author.ID, len(matches))
	} else if commandRe.Match([]byte(m.Content)) {
		handleCommand(s, m)
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

	commandRe = regexp.MustCompile(fmt.Sprintf("<@.%s>", discord.State.User.ID))

	discord.UpdateStatus(1, "@me help")

	// Wait here until CTRL-C or other term signal is received.
	fmt.Println("Bot is now running.  Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc

	// Cleanly close down the Discord session.
	discord.Close()

}
