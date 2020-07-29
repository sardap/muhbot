package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/go-audio/audio"
	"github.com/go-audio/wav"
	"github.com/jonas747/dca"
	cmap "github.com/orcaman/concurrent-map"
	"github.com/pkg/errors"
	"github.com/sardap/discgov"
	"gopkg.in/hraban/opus.v2"
)

type muhInfo struct {
	kill bool
}

//GuildWatcher GuildWatcher
type GuildWatcher struct {
	activeCh      string
	lock          *sync.RWMutex
	conn          *discordgo.VoiceConnection
	mCh           chan muhInfo
	userIDSsrcMap cmap.ConcurrentMap
}

//GuildInfo GuildInfo
type GuildInfo struct {
	data map[string]*GuildWatcher
	lock *sync.Mutex
}

type responseAudioProcessor struct {
	SayMuh bool `json:"say_muh"`
}

const sampleRate = 48000
const channels = 1 // mono; 2 for stereo

var (
	muhFilePath string
)

func init() {
	muhFilePath = os.Getenv("MUH_FILE_PATH")
}

func (g *GuildInfo) GetGuild(gID string) *GuildWatcher {
	g.lock.Lock()
	defer g.lock.Unlock()
	result, ok := g.data[gID]
	if !ok {
		result = &GuildWatcher{
			lock: &sync.RWMutex{}, mCh: make(chan muhInfo),
		}

		g.data[gID] = result
	}

	return result
}

func (g *GuildWatcher) sayMuhLister() {
	playMuh := func() {
		encodeSession, err := dca.EncodeFile(muhFilePath, dca.StdEncodeOptions)
		if err != nil {
			panic(err)
		}
		defer encodeSession.Cleanup()

		g.conn.Speaking(true)
		done := make(chan error)
		dca.NewStream(encodeSession, g.conn, done)
		err = <-done
		if err != nil && err != io.EOF {
		}
		g.conn.Speaking(false)
	}

	for muh := range g.mCh {
		if muh.kill {
			return
		}
		playMuh()
	}
}

func encodeFile(pcm []int16) (string, error) {
	filename := fmt.Sprintf(
		"%s.%s",
		filepath.Join(audioDumpPath, fmt.Sprintf("%d", rand.Int())), "wav",
	)

	out, err := os.Create(filename)
	if err != nil {
		return "", err
	}
	defer out.Close()

	// 8 kHz, 16 bit, 1 channel, WAV.
	e := wav.NewEncoder(out, sampleRate, 16, 1, 1)

	buf := &audio.IntBuffer{
		Format: &audio.Format{
			NumChannels: 1,
			SampleRate:  sampleRate,
		},
	}
	for _, sample := range pcm {
		buf.Data = append(buf.Data, int(sample))
	}

	// Write buffer to output file. This writes a RIFF header and the PCM chunks from the audio.IntBuffer.
	if err := e.Write(buf); err != nil {
		return "", err
	}
	if err := e.Close(); err != nil {
		return "", err
	}

	return filename, nil
}

var errAudioServerProcessing error = errors.New("500 error processing audio")

func sendForProcessing(filename string) (bool, error) {
	file, err := os.Open(filename)
	if err != nil {
		return false, errors.Wrap(err, "unable to open file")
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return false, errors.Wrap(err, "unable to add file to body")
	}

	url := fmt.Sprintf("%s/api/process_audio_file", audioProcessorEndpoint)
	io.Copy(part, file)
	writer.Close()
	request, err := http.NewRequest("POST", url, body)
	if err != nil {
		return false, errors.Wrap(err, "unable to create request")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(2)*time.Second)
	defer cancel()
	request.WithContext(ctx)
	request.Header.Add("Content-Type", writer.FormDataContentType())
	client := &http.Client{}

	response, err := client.Do(request)
	if err != nil {
		return false, errors.Wrap(err, "unable to make request")
	}
	defer response.Body.Close()

	if response.StatusCode == 500 {
		log.Printf("response %v\n", response)
		return false, errAudioServerProcessing
	}

	if response.StatusCode >= 400 && response.StatusCode <= 499 {
		return false, fmt.Errorf("Cannot load file 400 from server")
	}

	content, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return false, errors.Wrap(err, fmt.Sprintf("unable read request response %v", response))
	}

	var data responseAudioProcessor
	err = json.Unmarshal(content, &data)
	if err != nil {
		return false, errors.Wrap(err, fmt.Sprintf("unable unmarsahl request respons e%v", response))
	}

	return data.SayMuh, nil
}

func processSample(completePCM []int16, ch chan muhInfo) {
	filename, err := encodeFile(completePCM)
	if err != nil {
		panic(err)
	}
	defer os.Remove(filename)

	sayMuh, err := sendForProcessing(filename)
	if err != nil {
		log.Printf("Error fuck you %v\n", err)
		switch err {
		case errAudioServerProcessing:
			log.Printf("500 server error on python server")
			return
		default:
			log.Printf("error processing audio %v", err)
			return
		}
	}

	if sayMuh {
		ch <- muhInfo{}
	}
}

func (g *GuildWatcher) listenVoice() {
	inCh := g.conn.OpusRecv
	mCh := g.mCh

	dec, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		panic(err)
	}

	type voiceStream struct {
		pcm        []int16
		time       time.Duration
		emptyCount int
	}
	var frameSizeMs float32 = 60
	frameSize := channels * frameSizeMs * sampleRate / 1000
	pktMap := make(map[uint32]*voiceStream)
	for {
		select {
		case packet := <-inCh:
			log.Printf(
				"Packet SSRC:%d Sequence:%d timestamp:%d \n",
				packet.SSRC, packet.Sequence, packet.Timestamp,
			)
			pcm := make([]int16, int(frameSize))
			n, err := dec.Decode(packet.Opus, pcm)
			if err != nil {
				log.Printf("error:%v\n", err)
				break
			}

			// To get all samples (interleaved if multiple channels):
			pcm = pcm[:n*channels] // only necessary if you didn't know the right frame size

			val, ok := pktMap[packet.SSRC]
			if !ok {
				val = &voiceStream{
					time: time.Duration(0), pcm: make([]int16, 0),
					emptyCount: 0,
				}
				pktMap[packet.SSRC] = val
			}

			// or access sample per sample, directly:
			for i := 0; i < n; i++ {
				val.pcm = append(val.pcm, pcm[i*channels+0])
			}
			val.time += time.Duration(60) * time.Millisecond
			val.emptyCount = 0

		case <-time.After(time.Duration(60) * time.Millisecond):
			log.Printf("NO PACKETS\n")
			for _, v := range pktMap {
				v.emptyCount++
			}
		}

		deleteQueue := make([]uint32, 0)
		for k, v := range pktMap {
			log.Printf("time %v, empty:%d\n", v.time, v.emptyCount)
			if v.time > time.Duration(5)*time.Second || v.emptyCount > 20 {
				log.Printf("sending %v\n", k)
				go processSample(v.pcm, mCh)
				deleteQueue = append(deleteQueue, k)
			}
		}

		for _, k := range deleteQueue {
			delete(pktMap, k)
		}
	}
}

//ConnectToChannel ConnectToChannel
func (g *GuildWatcher) ConnectToChannel(s *discordgo.Session, guildID, targetChannel string) error {
	g.lock.RLock()
	defer g.lock.RUnlock()
	if g.conn != nil {
		g.DisconnectFromChannel()
	}

	g.mCh = make(chan muhInfo)
	g.activeCh = targetChannel
	var err error
	g.conn, err = s.ChannelVoiceJoin(guildID, g.activeCh, false, false)
	if err != nil {
		return err
	}
	g.conn.AddHandler(voiceStatusUpdate)
	go g.sayMuhLister()
	go g.listenVoice()

	return nil
}

//DisconnectFromChannel DisconnectFromChannel
func (g *GuildWatcher) DisconnectFromChannel() error {
	g.lock.RLock()
	defer g.lock.RUnlock()
	close(g.mCh)
	err := g.conn.Disconnect()
	g.conn = nil
	return err
}

func voiceStatusUpdate(vc *discordgo.VoiceConnection, vs *discordgo.VoiceSpeakingUpdate) {
	// g := gInfo.GetGuild(v.GuildID)
	// g.lock.Lock()
	// defer g.lock.Unlock()
	// g.userIDSsrcMap.Set(vs.SSRC, vs.UserID)
}

//VoiceStateUpdate VoiceStateUpdate
func VoiceStateUpdate(s *discordgo.Session, v *discordgo.VoiceStateUpdate) {
	if s.State.User.ID == v.UserID {
		return
	}

	discgov.UserVoiceTrackerHandler(s, v)

	g := gInfo.GetGuild(v.GuildID)
	g.lock.Lock()
	defer g.lock.Unlock()

	if g.conn == nil {
		return
	}

	curCount := len(discgov.GetUsers(v.GuildID, g.activeCh))

	if curCount == 0 {
		s.ChannelVoiceJoin(v.GuildID, "", false, false)
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
			// if g.conn == nil {
			// 	g.conn, _ = s.ChannelVoiceJoin(v.GuildID, v.ChannelID, false, false)
			// 	g.activeCh = bestCID
			// 	go listenVoice(g.conn, g.conn.OpusRecv)
			// } else {
			// 	g.conn.ChangeChannel(bestCID, false, false)
			// }
		} else if g.conn != nil {
			g.conn.Disconnect()
			close(g.conn.OpusRecv)
			g.conn = nil
		}
	}
}
