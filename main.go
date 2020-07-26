package main

import (
	"context"
	"fmt"
	"io"
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

	speech "cloud.google.com/go/speech/apiv1"
	"github.com/bwmarrin/discordgo"
	"github.com/go-redis/redis"
	"github.com/sardap/discgov"
	speechpb "google.golang.org/genproto/googleapis/cloud/speech/v1"
)

type guildWatcher struct {
	activeCh string
	lock     *sync.Mutex
	conn     *discordgo.VoiceConnection
}

type guildInfo struct {
	data map[string]*guildWatcher
	lock *sync.Mutex
}

// NOTE commandRe is set in main
var (
	meRe         *regexp.Regexp
	commandRe    *regexp.Regexp
	client       *redis.Client
	gInfo        *guildInfo
	gSpeakAPIKey string
)

func init() {
	meRe = regexp.MustCompile("(?P<me>me)(([.?!]|$)?(?P<form>[*_~]+)?)\"?'?([.?!\\r\\n\"']|$)")

	gSpeakAPIKey = os.Getenv("GOOGLE_SPEECH_API_KEY")

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

	gInfo = &guildInfo{
		lock: &sync.Mutex{}, data: make(map[string]*guildWatcher),
	}
}

func (g *guildInfo) getGuild(gID string) *guildWatcher {
	g.lock.Lock()
	defer g.lock.Unlock()
	result, ok := g.data[gID]
	if !ok {
		result = &guildWatcher{
			lock: &sync.Mutex{},
		}

		g.data[gID] = result
	}

	return result
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
					"\"<@%s> muh stats\" to show how many times you have said **the** word.",
				m.Author.ID, s.State.User.ID,
			),
		)
	} else if regexp.MustCompile("hear.*?i").Match([]byte(strings.ToLower(m.Content))) {
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

		gV := gInfo.getGuild(m.GuildID)
		gV.lock.Lock()
		defer gV.lock.Unlock()

		if gV.conn == nil {
			gV.conn, _ = s.ChannelVoiceJoin(m.GuildID, targetCh, false, false)
		} else {
			gV.conn.ChangeChannel(targetCh, false, false)
		}

		go listenVoice(gV.conn.OpusRecv)
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
		s.ChannelMessageSend(m.ChannelID, Muhafier(m.Content, m.Author.ID, matches))
		go logMuh(m.GuildID, m.Author.ID, len(matches))
	} else if strings.Contains(m.Content, s.State.User.ID) {
		handleCommand(s, m)
	}

	log.Printf("nice")
}

func listenVoice(ch chan *discordgo.Packet) {
	ctx := context.Background()
	c, err := speech.NewClient(ctx)
	if err != nil {
		panic(err)
	}
	stream, err := c.StreamingRecognize(ctx)
	if err != nil {
		panic(err)
	}
	req := &speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_StreamingConfig{
			StreamingConfig: &speechpb.StreamingRecognitionConfig{
				Config: &speechpb.RecognitionConfig{
					Encoding:                   speechpb.RecognitionConfig_OGG_OPUS,
					SampleRateHertz:            16000,
					AudioChannelCount:          1,
					LanguageCode:               "en-AU",
					MaxAlternatives:            10,
					ProfanityFilter:            false,
					EnableAutomaticPunctuation: false,
					UseEnhanced:                false,
				},
				InterimResults: true,
			},
		},
	}
	if err := stream.Send(req); err != nil {
		panic(err)
	}

	go func() {
		// content := make([]byte, 1024)
		// top := 0
		count := 0
		for packet := range ch {
			log.Printf("Sending new packet %d\n", count)
			req := &speechpb.StreamingRecognizeRequest{
				StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
					AudioContent: packet.Opus,
				},
			}
			if err := stream.Send(req); err != nil {
				panic(err)
			}

			if count > 100 {
				break
			}

			count++

			// top = 0
			// for i := 0; i < len(content); i++ {
			// 	content[i] = 0
			// }
		}
		stream.CloseSend()
	}()
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(err)
		}
		if err := resp.Error; err != nil {
			panic(fmt.Errorf("Could not recognize: %v", err))
		}
		for _, result := range resp.Results {
			log.Printf("Result: %+v\n", result)
		}
	}
	log.Printf("closed\n")
}

func voiceStateUpdate(s *discordgo.Session, v *discordgo.VoiceStateUpdate) {
	if s.State.User.ID == v.UserID {
		return
	}

	discgov.UserVoiceTrackerHandler(s, v)

	return

	g := gInfo.getGuild(v.GuildID)
	g.lock.Lock()
	defer g.lock.Unlock()

	curCount := len(discgov.GetUsers(v.GuildID, g.activeCh))

	if curCount == 0 {
		g.conn.Disconnect()
		return

		guild, _ := s.State.Guild(v.GuildID)

		best := 0
		bestCID := ""
		for _, ch := range guild.Channels {
			if ch.Bitrate == 0 {
				continue
			}

			count := len(discgov.GetUsers(v.GuildID, ch.ID))
			if count > best {
				bestCID = ch.ID
				best = count
			}
		}

		if bestCID != "" {
			if g.conn == nil {
				g.conn, _ = s.ChannelVoiceJoin(v.GuildID, v.ChannelID, false, false)
				g.activeCh = bestCID
				go listenVoice(g.conn.OpusRecv)
			} else {
				g.conn.ChangeChannel(bestCID, false, false)
			}
		} else if g.conn != nil {
			g.conn.Disconnect()
			close(g.conn.OpusRecv)
			g.conn = nil
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

	// Register the messageCreate func as a callback for MessageCreate events.
	discord.AddHandler(voiceStateUpdate)
	discord.AddHandler(messageCreate)

	// Open a websocket connection to Discord and begin listening.
	err = discord.Open()
	if err != nil {
		fmt.Println("error opening connection,", err)
		return
	}

	commandRe = regexp.MustCompile(fmt.Sprintf("<@[&!]?%s>", discord.State.User.ID))

	discord.UpdateStatus(1, "@me help")

	// Wait here until CTRL-C or other term signal is received.
	fmt.Println("Bot is now running.  Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc

	// Cleanly close down the Discord session.
	discord.Close()

}
