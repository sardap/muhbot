package main

import (
	"fmt"
	"log"
	"math/rand"
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
	"github.com/sardap/discom"
)

const MePattern = "(?P<me>me)(([.?!]|$)?(?P<form>[\x60*_~|]+)?)\"?'?\\)?([.?!\\r\\n\"']|$)"

// NOTE commandRe is set in main
var (
	meRe                   *regexp.Regexp
	brokenRe               *regexp.Regexp
	client                 *redis.Client
	gSpeakAPIKey           string
	audioDumpPath          string
	audioProcessorEndpoint string
)

func init() {
	meRe = regexp.MustCompile(MePattern)
	brokenRe = regexp.MustCompile("broken bot smh")

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

func errorHandler(s *discordgo.Session, i discom.Interaction, err error) {
	i.Respond(s, discom.Response{
		Content: fmt.Sprintf(
			"invalid command:\"%s\" error:%s",
			i.GetPayload().Message, err,
		),
	})
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
	messageBuilder.Grow(len(matches) * 4)
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
					strings.ToUpper(string(muhStr[0])), muhStr[1:],
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

func statsCommand(s *discordgo.Session, i discom.Interaction) error {
	i.Respond(s, discom.Response{
		Content: "processing",
	})

	res := client.Get(userKey(i.GetPayload().GuildId, i.GetPayload().AuthorId))
	if res.Val() == "" {
		i.Respond(s, discom.Response{
			Content: "you have never **the** word since I started writing it down.",
		})
		return nil
	}

	n, err := strconv.Atoi(res.Val())
	if err != nil {
		log.Printf("error getting data from DB %v\n", err)
		return err
	}

	i.Respond(s, discom.Response{
		Content: fmt.Sprintf(
			"you have said **the** word %d times (since I started writing it down)",
			n,
		),
	})

	return nil
}

func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	matches := meRe.FindAllStringSubmatchIndex(strings.ToLower(m.Content), -1)
	if len(matches) > 0 {
		s.ChannelMessageSendReply(m.ChannelID, Muhafier(m.Content, m.Author.ID, matches), m.Reference())
		go logMuh(m.GuildID, m.Author.ID, len(matches))
	}

	if m.Author.ID == "158496062103879681" && brokenRe.Match([]byte(strings.ToLower(m.Content))) {
		if rand.Intn(1000) == 999 {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("<@158496062103879681>, where's the DOTA bot?"))
		} else {
			s.ChannelMessageSend(m.ChannelID, fmt.Sprintf("<@158496062103879681>, you are the one who's broken"))
		}
	}
}

func main() {
	token := strings.Replace(os.Getenv("DISCORD_AUTH"), "\"", "", -1)
	discord, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Printf("unable to create new discord instance")
		log.Fatal(err)
	}

	commandSet, _ := discom.CreateCommandSet("-muh", errorHandler)
	commandSet.AddCommand(discom.Command{
		Name:        "stats",
		Handler:     statsCommand,
		Description: "get muh stats",
	})
	if err != nil {
		panic(err)
	}

	// Register the messageCreate func as a callback for MessageCreate events.
	discord.AddHandler(messageCreate)

	// Register the messageCreate func as a callback for MessageCreate events.
	discord.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		s.UpdateListeningStatus("try -muh or slash commands help")
		log.Println("Bot is up!")
	})

	discord.AddHandler(commandSet.Handler)
	discord.AddHandler(commandSet.IntreactionHandler)

	// Open a websocket connection to Discord and begin listening.
	if err := discord.Open(); err != nil {
		log.Fatal("error opening connection,", err)
	}
	defer discord.Close()

	commandSet.SyncAppCommands(discord)

	// Wait here until CTRL-C or other term signal is received.
	fmt.Println("Bot is now running.  Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc
}
