package main

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alecthomas/kong"
)

const (
	defaultFreq  = 10000.0 // kHz, ~10 MHz SW band
	stepBig      = 5.0     // kHz, +/- keys
	maxRandomTry = 3       // attempts when --random picks a server
)

// serverPool is a curated set of public KiwiSDR receivers used by --random.
// Extend freely; unreachable servers are skipped.
var serverPool = []string{
	"alg.twrmon.net:8073",
	"kiwisdr.njctech.com:8073",
	"kiwisdr.areg.org.au:8073",
	"22033.proxy.kiwisdr.com:8073",
}

// swBand is a known-active shortwave band with a sensible default mode.
type swBand struct {
	lo, hi float64 // kHz
	mode   string
}

var swBands = []swBand{
	{5900, 6200, "am"},    // 49m broadcast
	{7200, 7450, "am"},    // 41m broadcast
	{9400, 9900, "am"},    // 31m broadcast
	{11600, 12100, "am"},  // 25m broadcast
	{13570, 13870, "am"},  // 22m broadcast
	{15100, 15800, "am"},  // 19m broadcast
	{17480, 17900, "am"},  // 16m broadcast
	{18900, 19020, "am"},  // 15m broadcast
	{21450, 21850, "am"},  // 13m broadcast
	{25600, 26100, "am"},  // 11m broadcast / CB
	{7000, 7200, "lsb"},   // 40m ham
	{10000, 10150, "usb"}, // 30m ham / time standards
	{14000, 14350, "usb"}, // 20m ham
	{21000, 21450, "usb"}, // 15m ham
}

// pickRandomFreq returns a random frequency (kHz) from a known band, clamped to
// the receiver's tunable range so the resulting channel is always valid.
func pickRandomFreq(maxKhz float64) (freq float64, mode string) {
	if maxKhz <= 0 {
		maxKhz = 30000
	}
	for i := 0; i < 32; i++ {
		b := swBands[rand.Intn(len(swBands))]
		if b.lo >= maxKhz {
			continue
		}
		hi := b.hi
		if hi > maxKhz {
			hi = maxKhz
		}
		if hi <= b.lo {
			continue
		}
		f := b.lo + rand.Float64()*(hi-b.lo)
		return math.Round(f*10) / 10, b.mode
	}
	f := rand.Float64() * maxKhz
	return math.Round(f*10) / 10, "am"
}

type CLI struct {
	Random bool     `kong:"optional,name='random',help='Pick a random public KiwiSDR server and a random valid frequency'"`
	Mode   *string  `kong:"optional,short='m',help='Demodulation mode: am, amn, usb, usn, lsb, lsn, cw, cwn, nbfm, nnfm, sam (default am; auto-selected with --random)'"`
	Volume int      `kong:"optional,short='v',default='80',help='Volume 0-100'"`
	Test   bool     `kong:"optional,name='test',help='Connect, verify the protocol, then exit (no audio)'"`
	Server string   `kong:"arg,optional,help='KiwiSDR server, e.g. kiwisdr.ucsd.edu:8073'"`
	Freq   *float64 `kong:"optional,short='f',help='Start frequency in kHz (default 10000; random with --random)'"`
}

type app struct {
	mu        sync.Mutex
	freq      float64
	mode      string
	vol       int
	smeter    int
	rate      int
	client    *KiwiClient
	player    *Player
	connected bool
}

func (a *app) setConnected() {
	a.mu.Lock()
	a.connected = true
	a.mu.Unlock()
}

func (a *app) statusText() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := fmt.Sprintf("freq=%7.1f kHz  mode=%-4s  vol=%3d%%  sig=%4d  rate=%d Hz",
		a.freq, a.mode, a.vol, a.smeter, a.rate)
	if !a.connected {
		s += "  [connecting...]"
	}
	return s
}

// dialServer connects to the requested server, or to a random server from the
// pool when --random is used (with a few retries for unreachable receivers).
func dialServer(cli *CLI) (*KiwiClient, string, error) {
	if !cli.Random || cli.Server != "" {
		if cli.Server == "" {
			return nil, "", fmt.Errorf("no server address given")
		}
		c, err := Dial(cli.Server)
		return c, normalizeServer(cli.Server), err
	}

	order := rand.Perm(len(serverPool))
	var lastErr error
	for i, idx := range order {
		if i >= maxRandomTry {
			break
		}
		srv := serverPool[idx]
		fmt.Printf("trying random server %s ...\n", srv)
		c, err := Dial(srv)
		if err == nil {
			fmt.Printf("connected to %s\n", srv)
			return c, srv, nil
		}
		lastErr = err
		fmt.Printf("  %s is unreachable: %v\n", srv, err)
	}
	return nil, "", fmt.Errorf("no public server could be reached: %w", lastErr)
}

func main() {
	var cli CLI
	kong.Parse(&cli,
		kong.Name("shortwave"),
		kong.Description("Listen to shortwave radio over the internet via a remote KiwiSDR.\n\n"+
			"Usage:\n  shortwave [<server>] [flags]\n\n"+
			"While listening:  q quit, +/- step 5 kHz, 1/2 step 1 kHz, <number> tune to kHz,\n"+
			"  m <mode> change mode, v <0-100> volume, ? status\n"+
			"\nExamples:\n  shortwave alg.twrmon.net:8073 -f 9650 -m am\n"+
			"  shortwave --random --volume 50"),
		kong.UsageOnError(),
	)

	if cli.Server == "" && !cli.Random {
		fmt.Fprintln(os.Stderr, "shortwave: no server address given (or use --random)")
		fmt.Fprintln(os.Stderr, "try 'shortwave --help'")
		os.Exit(1)
	}
	if cli.Volume < 0 {
		cli.Volume = 0
	}
	if cli.Volume > 100 {
		cli.Volume = 100
	}

	client, server, err := dialServer(&cli)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shortwave:", err)
		os.Exit(1)
	}
	defer client.Close()

	if cli.Test {
		os.Exit(runTest(client, cli))
	}

	// Resolve starting mode and frequency (a random freq needs the server's
	// reported range, so it's chosen after we learn the sample rate).
	mode := "am"
	if cli.Mode != nil {
		mode = *cli.Mode
	}
	freq := defaultFreq
	if cli.Freq != nil {
		freq = *cli.Freq
	}
	if !isMode(mode) {
		fmt.Fprintf(os.Stderr, "shortwave: unknown mode %q, using am\n", mode)
		mode = "am"
	}

	a := &app{
		freq: freq,
		mode: mode,
		vol:  cli.Volume,
	}
	a.client = client

	rateCh := make(chan int, 1)
	errCh := make(chan error, 1)
	var errOnce sync.Once

	client.OnError = func(err error) {
		errOnce.Do(func() { errCh <- err })
	}
	client.OnRate = func(rate int) {
		select {
		case rateCh <- rate:
		default:
		}
	}
	client.OnSmeter = func(s int) {
		a.mu.Lock()
		a.smeter = s
		a.mu.Unlock()
	}
	client.OnAudio = func(payload []byte, flags byte) {
		if a.player == nil {
			return
		}
		var out [2]int16
		samples := make([]float64, 0, len(payload)*2)
		if flags&sndFlagCompressed != 0 {
			for _, b := range payload {
				client.adpcm.decodeByte(b, out[:])
				samples = append(samples, float64(out[0])/32768.0, float64(out[1])/32768.0)
			}
		} else {
			le := flags&sndFlagLittleEndian != 0
			for i := 0; i+1 < len(payload); i += 2 {
				var s int16
				if le {
					s = int16(payload[i]) | int16(payload[i+1])<<8
				} else {
					s = int16(payload[i])<<8 | int16(payload[i+1])
				}
				samples = append(samples, float64(s)/32768.0)
			}
		}
		a.player.Push(samples)
	}

	client.Start()
	if err := client.Auth(); err != nil {
		fmt.Fprintln(os.Stderr, "shortwave:", err)
		os.Exit(1)
	}
	a.setConnected()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Keepalive every 5s so the server never times us out.
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				client.Send("SET keepalive")
			}
		}
	}()

	// Wait for the audio sample rate (also confirms auth succeeded).
	var rate int
	select {
	case rate = <-rateCh:
	case err := <-errCh:
		fmt.Fprintln(os.Stderr, "shortwave:", err)
		os.Exit(1)
	case <-time.After(30 * time.Second):
		fmt.Fprintln(os.Stderr, "shortwave: timed out waiting for the server")
		os.Exit(1)
	}

	player, err := NewPlayer(rate)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shortwave: audio output failed:", err)
		os.Exit(1)
	}
	defer player.Close()
	a.player = player
	a.mu.Lock()
	a.rate = rate
	a.mu.Unlock()
	player.SetVolume(a.vol)

	// With --random, pick a valid frequency within this receiver's range.
	if cli.Random {
		a.mu.Lock()
		a.freq, a.mode = pickRandomFreq(client.MaxKhz)
		if cli.Mode != nil {
			a.mode = *cli.Mode
		}
		a.mu.Unlock()
		fmt.Printf("random: %s at %g kHz %s\n", server, a.freq, a.mode)
	}

	if err := client.Tune(a.mode, a.freq); err != nil {
		fmt.Fprintln(os.Stderr, "shortwave:", err)
		os.Exit(1)
	}
	client.SetAgc()
	client.ArOk(rate, int(outRate))

	// Signal meter ticker.
	statusDone := make(chan struct{})
	go func() {
		t := time.NewTicker(1 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				fmt.Printf("\r%s", a.statusText())
			case <-statusDone:
				return
			}
		}
	}()
	defer close(statusDone)

	// Watch for connection errors in the background.
	watchDone := make(chan struct{})
	go func() {
		select {
		case err := <-errCh:
			fmt.Fprintf(os.Stderr, "\nshortwave: connection lost: %v\n", err)
			cancel()
		case <-watchDone:
		}
	}()
	defer close(watchDone)

	fmt.Println()
	fmt.Println(a.statusText())
	fmt.Println("commands: q quit | +/- 5kHz | 1/2 1kHz | <number> kHz | m <mode> | v <0-100> | ? status")
	interactive(&cli, a, ctx)
}

func interactive(cli *CLI, a *app, ctx context.Context) {
	reader := bufio.NewReader(os.Stdin)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.TrimSpace(line)
		switch {
		case cmd == "q" || cmd == "quit" || cmd == "exit":
			return
		case cmd == "?" || cmd == "" || cmd == "status":
			fmt.Println(a.statusText())
		case cmd == "+" || cmd == "up":
			a.tune(a.freq + stepBig)
		case cmd == "-" || cmd == "down":
			a.tune(a.freq - stepBig)
		case cmd == "1":
			a.tune(a.freq + 1)
		case cmd == "2":
			a.tune(a.freq - 1)
		case strings.HasPrefix(cmd, "m "):
			m := strings.TrimSpace(cmd[2:])
			if isMode(m) {
				a.mu.Lock()
				a.mode = m
				a.mu.Unlock()
				a.tune(a.freq)
			} else {
				fmt.Printf("unknown mode %q (valid: %s)\n", m, strings.Join(validModes, ", "))
			}
		case strings.HasPrefix(cmd, "v "):
			if n, err := strconv.Atoi(strings.TrimSpace(cmd[2:])); err == nil && n >= 0 && n <= 100 {
				a.mu.Lock()
				a.vol = n
				a.mu.Unlock()
				a.player.SetVolume(n)
			} else {
				fmt.Println("volume must be 0-100")
			}
		default:
			if f, err := strconv.ParseFloat(cmd, 64); err == nil {
				a.tune(f)
			} else {
				fmt.Printf("unknown command %q (try '?')\n", cmd)
			}
		}
	}
}

func (a *app) tune(f float64) {
	if f < 0 {
		f = 0
	}
	a.mu.Lock()
	a.freq = f
	mode := a.mode
	a.mu.Unlock()
	if err := a.client.Tune(mode, f); err != nil {
		fmt.Fprintln(os.Stderr, "shortwave:", err)
	}
}

// runTest connects, authenticates, tunes, and verifies audio frames arrive,
// then exits. Returns the process exit code.
func runTest(client *KiwiClient, cli CLI) int {
	msgs := 0
	frames := 0
	comp := 0
	badp := ""
	errCh := make(chan error, 1)
	rateCh := make(chan int, 1)

	client.OnMessage = func(line string) {
		msgs++
		if msgs <= 25 {
			short := line
			if len(short) > 120 {
				short = short[:120] + "..."
			}
			fmt.Println("MSG:", short)
		}
		if len(line) > 9 && line[:8] == "MSG badp" {
			badp = line
		}
	}
	client.OnRate = func(r int) {
		rateCh <- r
	}
	client.OnSmeter = func(s int) { _ = s }
	client.OnAudio = func(payload []byte, flags byte) {
		frames++
		if flags&sndFlagCompressed != 0 {
			comp++
		}
	}
	client.OnError = func(err error) {
		select {
		case errCh <- err:
		default:
		}
	}

	client.Start()
	if err := client.Auth(); err != nil {
		fmt.Fprintln(os.Stderr, "auth:", err)
		return 1
	}

	mode := "am"
	if cli.Mode != nil {
		mode = *cli.Mode
	}
	freq := defaultFreq
	if cli.Freq != nil {
		freq = *cli.Freq
	}

	// Wait for the sample rate, then send the full startup command set.
	var gotRate int
	select {
	case r := <-rateCh:
		gotRate = r
		if err := client.Tune(mode, freq); err != nil {
			fmt.Fprintln(os.Stderr, "tune:", err)
			return 1
		}
		client.SetAgc()
		client.ArOk(r, int(outRate))
	case err := <-errCh:
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		return 1
	case <-time.After(30 * time.Second):
		fmt.Fprintln(os.Stderr, "timed out waiting for sample rate")
		return 1
	}

	fmt.Printf("connected to %s, tuned to %g kHz %s at %d Hz ...\n", normalizeServer(cli.Server), freq, mode, gotRate)

	deadline := time.After(12 * time.Second)
	rate := gotRate
	for {
		select {
		case r := <-rateCh:
			rate = r
		case err := <-errCh:
			fmt.Fprintln(os.Stderr, "ERROR:", err)
			return 1
		case <-deadline:
			fmt.Printf("\nresult: msgs=%d frames=%d compressed=%d rate=%d", msgs, frames, comp, rate)
			if badp != "" {
				fmt.Printf(" (%s)", badp)
			}
			fmt.Println()
			if frames > 20 && rate > 0 {
				fmt.Println("OK: protocol verified")
				return 0
			}
			fmt.Println("FAIL: not enough audio frames")
			return 1
		}
	}
}
