package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"

	// "mime/multipart"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/go-audio/audio"
	"github.com/go-audio/wav"
	"github.com/jonas747/dca"
	cmap "github.com/orcaman/concurrent-map"
	"github.com/orcaman/writerseeker"
	"github.com/pkg/errors"
	"github.com/sardap/discgov"
	"gopkg.in/hraban/opus.v2"
)

type muhInfo struct {
	kill bool
}

type encoderInfo struct {
	pcm    []int16
	length time.Duration
}

//GuildWatcher GuildWatcher
type GuildWatcher struct {
	activeCh      string
	lock          *sync.RWMutex
	conn          *discordgo.VoiceConnection
	mCh           chan muhInfo
	enCh          chan encoderInfo
	userIDSsrcMap cmap.ConcurrentMap
	ctxCancel     context.CancelFunc
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

//GetGuild GetGuild
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

func (g *GuildWatcher) sayMuhLister(ctx context.Context) {
	playMuh := func() {
		encodeSession, err := dca.EncodeFile(muhFilePath, dca.StdEncodeOptions)
		if err != nil {
			log.Printf("unable to encode file %v\n", err)
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

	for {
		select {
		case <-g.mCh:
			playMuh()
		case <-ctx.Done():
			log.Printf("Killing say muh")
			return
		}
	}
}

func (g *GuildWatcher) encodeConsumer(ctx context.Context) {
	completePCM := make([]int16, 0)
	length := time.Duration(0)
	for {
		select {
		case toEn := <-g.enCh:
			for _, sample := range toEn.pcm {
				completePCM = append(completePCM, sample)
			}

			length += toEn.length
			if length > time.Duration(16)*time.Second {
				go processSample(completePCM, g.mCh)
				completePCM = make([]int16, 0)
				length = 0
			}
		case <-ctx.Done():
			log.Printf("Killing encode consumer")
			return
		}
	}
}

func encodeToWav(pcm []int16) (io.Reader, error) {
	writerSeeker := &writerseeker.WriterSeeker{}
	// 8 kHz, 16 bit, 1 channel, WAV.
	e := wav.NewEncoder(writerSeeker, sampleRate, 16, 1, 1)

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
		return nil, err
	}
	if err := e.Close(); err != nil {
		return nil, err
	}

	return writerSeeker.Reader(), nil
}

var errAudioServerProcessing error = errors.New("500 error processing audio")

func sendForProcessing(reader io.Reader) (bool, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "temp")
	if err != nil {
		return false, errors.Wrap(err, "unable to add file to body")
	}

	io.Copy(part, reader)
	writer.Close()

	buf := new(bytes.Buffer)
	buf.ReadFrom(reader)

	url := fmt.Sprintf("%s/api/process_audio_file", audioProcessorEndpoint)
	request, err := http.NewRequest("POST", url, body)
	if err != nil {
		return false, errors.Wrap(err, "unable to create request")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(5)*time.Second)
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
		return false, fmt.Errorf("CanNt load file 400 from server")
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
	buffer, err := encodeToWav(completePCM)
	if err != nil {
		panic(err)
	}

	sayMuh, err := sendForProcessing(buffer)
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

func (g *GuildWatcher) listenVoice(ctx context.Context) {
	inCh := g.conn.OpusRecv

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
		case <-ctx.Done():
			return
		}

		deleteQueue := make([]uint32, 0)
		for k, v := range pktMap {
			// log.Printf("time %v, empty:%d\n", v.time, v.emptyCount)
			if v.time > time.Duration(5)*time.Second || v.emptyCount > 20 {
				log.Printf("sending %v\n", k)
				if ctx.Err() == nil {
					g.enCh <- encoderInfo{v.pcm, v.time}
				}
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

	var err error
	g.conn, err = s.ChannelVoiceJoin(guildID, targetChannel, false, false)
	if err != nil {
		return err
	}
	g.conn.AddHandler(voiceStatusUpdate)
	g.mCh = make(chan muhInfo)
	g.enCh = make(chan encoderInfo)
	g.activeCh = targetChannel

	var ctx context.Context
	ctx, g.ctxCancel = context.WithCancel(context.Background())
	go g.sayMuhLister(ctx)
	go g.listenVoice(ctx)
	go g.encodeConsumer(ctx)
	return nil
}

//DisconnectFromChannel DisconnectFromChannel
func (g *GuildWatcher) DisconnectFromChannel() error {
	log.Printf("disconnecting\n")
	g.lock.RLock()
	defer g.lock.RUnlock()
	if g.conn == nil {
		return fmt.Errorf("no connection on this server")
	}

	g.ctxCancel()
	err := g.conn.Disconnect()
	g.conn = nil
	g.activeCh = ""
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
	g.lock.RLock()
	defer g.lock.RUnlock()

	if g.conn == nil {
		return
	}

	if len(discgov.GetUsers(v.GuildID, g.activeCh)) == 0 {
		g.DisconnectFromChannel()
	}
}
