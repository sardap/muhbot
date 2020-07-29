package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/bwmarrin/discordgo"
	"github.com/go-redis/redis"
	"github.com/sardap/discgov"
)

// NOTE commandRe is set in main
var (
	meRe                   *regexp.Regexp
	commandRe              *regexp.Regexp
	client                 *redis.Client
	gInfo                  *GuildInfo
	gSpeakAPIKey           string
	audioDumpPath          string
	audioProcessorEndpoint string
)

func init() {
	meRe = regexp.MustCompile("(?P<me>me)(([.?!]|$)?(?P<form>[*_~|]+)?)\"?'?\\)?([.?!\\r\\n\"']|$)")

	gSpeakAPIKey = os.Getenv("GOOGLE_SPEECH_API_KEY")
	audioDumpPath = os.Getenv("AUDIO_DUMP")
	audioProcessorEndpoint = os.Getenv("AUDIO_PROCESSOR_ENDPOINT")

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

	gInfo = &GuildInfo{
		lock: &sync.Mutex{}, data: make(map[string]*GuildWatcher),
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

func findGroupIdx(key string, keys []string) int {
	result := -1
	for i := 1; i < len(keys); i++ {
		if keys[i] == key {
			result = i * 2
			break
		}
	}

	return result
}

//Muhafier Muhafier
func Muhafier(message, authorID string, matches [][]int) string {
	var messageBuilder strings.Builder
	messageBuilder.Grow(len(matches)*4 + len("<@>") + len(authorID) + 1)
	fmt.Fprintf(&messageBuilder, "<@%s> ", authorID)
	keys := meRe.SubexpNames()
	meGroup := findGroupIdx("me", keys)
	formGroup := findGroupIdx("form", keys)

	for _, match := range matches {
		modify := ""
		if match[formGroup] > 0 {
			modify = message[match[formGroup]:match[formGroup+1]]
		}
		target := message[match[meGroup]:match[meGroup+1]]
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

		fmt.Fprintf(&messageBuilder, "%s%s%s ", modify, muhStr, reverse(modify))
	}

	return messageBuilder.String()
}

func handleCommand(s *discordgo.Session, m *discordgo.MessageCreate) {
	if regexp.MustCompile(".*?help$").Match([]byte(m.Content)) {
		s.ChannelMessageSend(
			m.ChannelID,
			fmt.Sprintf(
				"<@%s> Commands:\n"+
					"\"muh$ stats\" to show how many times you have said **the** word.\n"+
					"\"muh$ hear\" me muh is going to join you in a voice call.\n"+
					"\"muh$ fuck off\" me muh is going to leave if im in a call.\n",
				m.Author.ID, s.State.User.ID,
			),
		)
	} else if regexp.MustCompile("hear$").Match([]byte(strings.ToLower(m.Content))) {
		g, _ := s.State.Guild(m.GuildID)

		targetCh := ""
		for _, ch := range g.Channels {
			if ch.Bitrate == 0 {
				continue
			}

			for _, uID := range discgov.GetUsers(m.GuildID, ch.ID) {
				if uID == m.Author.ID {
					targetCh = ch.ID
					break
				}
			}
		}

		if targetCh == "" {
			s.ChannelMessageSend(
				m.ChannelID,
				fmt.Sprintf(
					"<@%s> you must be in a voice channel!\n",
					m.Author.ID,
				),
			)
			return
		}

		gV := gInfo.GetGuild(m.GuildID)
		gV.ConnectToChannel(s, m.GuildID, targetCh)

	} else if regexp.MustCompile("stats$").Match([]byte(m.Content)) {
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
	} else if regexp.MustCompile("fuck.*?off$").Match([]byte(m.Content)) {
		gV := gInfo.GetGuild(m.GuildID)
		gV.lock.RLock()
		defer gV.lock.RUnlock()

		err := gV.DisconnectFromChannel()
		if err != nil {
			s.ChannelMessageSend(
				m.ChannelID,
				fmt.Sprintf(
					"<@%s> error:%s.",
					m.Author.ID, err.Error(),
				),
			)
		}
	}
}

func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	log.Printf("ID: %s Message: %s\n", s.State.User.ID, m.Content)
	matches := meRe.FindAllStringSubmatchIndex(strings.ToLower(m.Content), -1)
	if len(matches) > 0 {
		s.ChannelMessageSend(m.ChannelID, Muhafier(m.Content, m.Author.ID, matches))
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
	discord.AddHandler(VoiceStateUpdate)
	discord.AddHandler(messageCreate)

	// Open a websocket connection to Discord and begin listening.
	err = discord.Open()
	if err != nil {
		fmt.Println("error opening connection,", err)
		return
	}

	commandRe = regexp.MustCompile("muh\\$")

	discord.UpdateStatus(1, "muh$ help")

	// Wait here until CTRL-C or other term signal is received.
	fmt.Println("Bot is now running.  Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc

	// Cleanly close down the Discord session.
	discord.Close()

}
