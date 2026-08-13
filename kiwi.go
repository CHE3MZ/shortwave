package main

import (
	"encoding/binary"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// KiwiSDR audio frame flags (byte 3 of each SND packet).
const (
	sndFlagStereo       = 0x08
	sndFlagCompressed   = 0x10
	sndFlagRestart      = 0x20
	sndFlagLittleEndian = 0x80
)

// validModes are the demodulation modes the server accepts.
var validModes = []string{"am", "amn", "amw", "usb", "usn", "lsb", "lsn", "cw", "cwn", "nbfm", "nnfm", "sam", "iq"}

func isMode(m string) bool {
	for _, v := range validModes {
		if v == m {
			return true
		}
	}
	return false
}

// passband returns low_cut/high_cut in Hz for a mode.
func passband(mode string) (lo, hi int) {
	switch mode {
	case "usb", "usn":
		return 300, 3000
	case "lsb", "lsn":
		return -3000, -300
	case "cw", "cwn":
		return -400, 400
	case "nbfm", "nnfm":
		return -8000, 8000
	default:
		return -4000, 4000
	}
}

type KiwiClient struct {
	ws      *websocket.Conn
	writeMu sync.Mutex

	sampleRate int

	// MaxKhz is the tunable top frequency in kHz, from the server's
	// center_freq/bandwidth message (set shortly after auth).
	MaxKhz float64

	adpcm adpcmState

	// OnAudio receives the payload bytes (after the 10-byte header) plus flags
	// and the current sample rate.
	OnAudio func(payload []byte, flags byte)
	// OnSmeter receives the 16-bit signal meter value.
	OnSmeter func(smeter int)
	// OnRate is called when the server reports the audio sample rate.
	OnRate func(rate int)
	// OnMessage is called with every raw control message (for debugging).
	OnMessage func(line string)
	// OnError is called once when the connection fails or is rejected.
	OnError func(err error)
}

func normalizeServer(s string) string {
	s = strings.TrimSpace(s)
	for _, p := range []string{"wss://", "ws://", "https://", "http://"} {
		if strings.HasPrefix(s, p) {
			s = strings.TrimPrefix(s, p)
			break
		}
	}
	s = strings.TrimRight(s, "/")
	if !strings.Contains(s, ":") {
		s += ":8073"
	}
	return s
}

// Dial connects to a KiwiSDR server and returns a client. The websocket is
// open but not yet authenticated.
func Dial(server string) (*KiwiClient, error) {
	host := normalizeServer(server)
	var lastErr error
	for _, scheme := range []string{"ws", "wss"} {
		u := url.URL{
			Scheme: scheme,
			Host:   host,
			Path:   fmt.Sprintf("/ws/kiwi/%d/SND", time.Now().UnixMilli()),
		}
		for _, withOrigin := range []bool{true, false} {
			hdr := http.Header{}
			hdr.Set("User-Agent", "shortwave/0.1")
			if withOrigin {
				hdr.Set("Origin", scheme+"://"+host)
			}
			ws, _, err := websocket.DefaultDialer.Dial(u.String(), hdr)
			if err == nil {
				return &KiwiClient{ws: ws}, nil
			}
			lastErr = err
		}
	}
	return nil, fmt.Errorf("cannot connect to %s: %w", host, lastErr)
}

func (c *KiwiClient) Send(cmd string) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.ws.WriteMessage(websocket.TextMessage, []byte(cmd))
}

func (c *KiwiClient) Auth() error {
	return c.Send("SET auth t=kiwi")
}

func (c *KiwiClient) Tune(mode string, freqKhz float64) error {
	lo, hi := passband(mode)
	return c.Send(fmt.Sprintf("SET mod=%s low_cut=%d high_cut=%d freq=%.3f", mode, lo, hi, freqKhz))
}

// SetAgc enables AGC. The server refuses to stream audio until it has
// received AGC and audio-rate commands.
func (c *KiwiClient) SetAgc() error {
	return c.Send("SET agc=1 hang=10 thresh=-90 slope=1 decay=50 manGain=80")
}

// ArOk acknowledges the audio sample rate; the server needs it before
// streaming audio.
func (c *KiwiClient) ArOk(in, out int) error {
	return c.Send(fmt.Sprintf("SET AR OK in=%d out=%d", in, out))
}

func (c *KiwiClient) Close() error { return c.ws.Close() }

// Start runs the read loop in the background until the connection drops.
func (c *KiwiClient) Start() {
	go c.readLoop()
}

func (c *KiwiClient) readLoop() {
	defer c.ws.Close()
	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			if c.OnError != nil {
				c.OnError(err)
			}
			return
		}
		// The server sends every message as a binary frame. Control messages
		// start with "MSG", audio packets start with "SND".
		if len(data) >= 3 {
			switch string(data[:3]) {
			case "MSG":
				c.handleText(string(data))
			case "SND":
				c.handleBinary(data)
			}
		}
	}
}

func (c *KiwiClient) handleText(s string) {
	if !strings.HasPrefix(s, "MSG ") {
		return
	}
	if c.OnMessage != nil {
		c.OnMessage(s)
	}
	for _, p := range strings.Split(strings.TrimSpace(s[4:]), " ") {
		eq := strings.IndexByte(p, '=')
		if eq < 0 {
			continue
		}
		key, val := p[:eq], p[eq+1:]
		switch key {
		case "audio_rate":
			if n, err := strconv.Atoi(val); err == nil {
				c.sampleRate = n
				if c.OnRate != nil {
					c.OnRate(n)
				}
			}
		case "bandwidth":
			if n, err := strconv.ParseFloat(val, 64); err == nil && n > 0 {
				c.MaxKhz = n / 1000.0 // bandwidth is in Hz
			}
		case "badp":
			if n, err := strconv.Atoi(val); err == nil && n != 0 {
				if c.OnError != nil {
					c.OnError(fmt.Errorf("server rejected the connection (badp=%d)", n))
				}
			}
		}
	}
}

func (c *KiwiClient) handleBinary(data []byte) {
	if len(data) < 10 || string(data[:3]) != "SND" {
		return
	}
	flags := data[3]
	if c.OnSmeter != nil {
		c.OnSmeter(int(binary.BigEndian.Uint16(data[8:10])))
	}

	offset := 10
	if flags&sndFlagStereo != 0 {
		offset = 20
	}
	if len(data) <= offset {
		return
	}
	payload := data[offset:]

	if flags&sndFlagRestart != 0 {
		c.adpcm.reset()
	}
	if c.OnAudio != nil {
		c.OnAudio(payload, flags)
	}
}
