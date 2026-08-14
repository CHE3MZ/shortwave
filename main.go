package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alecthomas/kong"
)

const (
	version      = "0.1.0"
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

// voiceBands are ham SSB phone bands where live human speech is active daily.
var voiceBands = []swBand{
	{3600, 4000, "lsb"},   // 80m
	{7125, 7300, "lsb"},   // 40m (best in the evening)
	{14150, 14350, "usb"}, // 20m
	{18110, 18168, "usb"}, // 17m
	{21300, 21450, "usb"}, // 15m
	{28300, 29700, "usb"}, // 10m
}

// pickFromBands returns a random frequency (kHz) from the given bands, clamped
// to the receiver's tunable range so the resulting channel is always valid.
func pickFromBands(bands []swBand, maxKhz float64) (freq float64, mode string) {
	if maxKhz <= 0 {
		maxKhz = 30000
	}
	for i := 0; i < 32; i++ {
		b := bands[rand.Intn(len(bands))]
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

// pickRandomFreq picks a random frequency on a general shortwave band.
func pickRandomFreq(maxKhz float64) (float64, string) { return pickFromBands(swBands, maxKhz) }

// pickVoiceFreq picks a random ham SSB phone frequency, where you will most
// likely hear live human speech.
func pickVoiceFreq(maxKhz float64) (float64, string) { return pickFromBands(voiceBands, maxKhz) }

type CLI struct {
	Random  bool     `kong:"optional,name='random',help='Pick a random public KiwiSDR server and a random valid frequency'"`
	Voice   bool     `kong:"optional,name='voice',help='Tune to a random ham SSB phone frequency where people talk'"`
	Example *int     `kong:"optional,name='example',help='Listen to a preset: 1 = 7222 kHz LSB @ vol 25 (default), 2 = 1010 kHz LSB @ vol 25'"`
	Scan    bool     `kong:"optional,name='scan',help='Sweep a band and list the strongest signals (no audio)'"`
	Band    string   `kong:"optional,name='band',help='With --scan: range to sweep, e.g. --band 7100-7300'"`
	Mode    *string  `kong:"optional,short='m',help='Demodulation mode: am, amn, usb, usn, lsb, lsn, cw, cwn, nbfm, nnfm, sam (default am; auto-selected otherwise)'"`
	Volume  *int     `kong:"optional,short='v',help='Volume 0-100 (default 80)'"`
	Version bool     `kong:"optional,name='version',help='Show version and exit'"`
	Test    bool     `kong:"optional,name='test',help='Connect, verify the protocol, then exit (no audio)'"`
	Server  string   `kong:"arg,optional,help='KiwiSDR server, e.g. kiwisdr.ucsd.edu:8073 (defaults to the last one used)'"`
	Freq    *float64 `kong:"optional,short='f',help='Start frequency in kHz (default 10000; random with --random/--voice)'"`
}

// normalizeArgs lets "--example" work bare as well as with a value:
// "--example" (no value) becomes "--example=1"; "--example 2" is left alone.
func normalizeArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--example" {
			if i+1 < len(args) {
				if _, err := strconv.Atoi(args[i+1]); err == nil {
					out = append(out, a)
					continue
				}
			}
			out = append(out, "--example=1")
			continue
		}
		out = append(out, a)
	}
	return out
}

// config persists the last server so it doesn't need to be typed each time.
type config struct {
	LastServer string  `json:"last_server,omitempty"`
	LastFreq   float64 `json:"last_freq,omitempty"`
	LastMode   string  `json:"last_mode,omitempty"`
}

func configPath() string {
	return filepath.Join(".", ".shortwave.json")
}

func loadConfig() *config {
	c := &config{}
	b, err := os.ReadFile(configPath())
	if err != nil {
		return c
	}
	json.Unmarshal(b, c)
	return c
}

func (c *config) save() {
	if b, err := json.MarshalIndent(c, "", "  "); err == nil {
		os.WriteFile(configPath(), b, 0o600)
	}
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

// dialServer connects to the requested server, a random pool server (when
// --random is used, avoiding the previous one), or the last-used server.
func dialServer(cli *CLI, cfg *config) (*KiwiClient, string, error) {
	// Explicit server always wins.
	if cli.Server != "" {
		c, err := Dial(cli.Server)
		return c, normalizeServer(cli.Server), err
	}

	// --random: try random pool servers, skipping the previous one.
	if cli.Random {
		pool := serverPool
		filtered := make([]string, 0, len(pool))
		for _, s := range pool {
			if s != cfg.LastServer {
				filtered = append(filtered, s)
			}
		}
		if len(filtered) == 0 {
			filtered = pool
		}
		order := rand.Perm(len(filtered))
		var lastErr error
		for i, idx := range order {
			if i >= maxRandomTry {
				break
			}
			srv := filtered[idx]
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

	// Otherwise reuse the last-used server.
	if cfg.LastServer != "" {
		fmt.Printf("reconnecting to last server %s (use --random or an address to change)\n", cfg.LastServer)
		c, err := Dial(cfg.LastServer)
		return c, cfg.LastServer, err
	}
	return nil, "", fmt.Errorf("no server address given (use --random, an address, or run once with both)")
}

func main() {
	os.Args = append([]string{os.Args[0]}, normalizeArgs(os.Args[1:])...)
	var cli CLI
	kong.Parse(&cli,
		kong.Name("shortwave"),
		kong.Description("Listen to shortwave radio over the internet via a remote KiwiSDR.\n\n"+
			"Usage:\n  shortwave [<server>] [flags]\n\n"+
			"While listening:  q quit, +/- step 5 kHz, 1/2 step 1 kHz, <number> tune to kHz,\n"+
			"  m <mode> change mode, v <0-100> volume, ? status\n"+
			"\n--example presets:\n  --example 1   ->   -f 7222 -m lsb -v 25\n"+
			"  --example 2   ->   -f 1010 -m lsb -v 25\n"+
			"\nExamples:\n  shortwave alg.twrmon.net:8073 -f 9650 -m am\n"+
			"  shortwave --random --volume 50"),
		kong.UsageOnError(),
	)

	if cli.Version {
		fmt.Printf("shortwave %s\n", version)
		os.Exit(0)
	}
	if cli.Example != nil && *cli.Example != 1 && *cli.Example != 2 && *cli.Example != 3 {
		fmt.Fprintf(os.Stderr, "shortwave: unknown --example value %d (valid: 1 , 2 , 3)\n", *cli.Example)
		os.Exit(1)
	}

	// --example presets.
	var exFreq float64
	exMode := ""
	exVol := 0
	if cli.Example != nil {
		switch *cli.Example {
		case 1:
			exFreq, exMode, exVol = 7222, "lsb", 25
		case 2:
			exFreq, exMode, exVol = 1010, "lsb", 25
		case 3:
			exFreq, exMode, exVol = 1030, "lsb", 25
		}
	}

	vol := 80
	if cli.Volume != nil {
		vol = *cli.Volume
	} else if cli.Example != nil {
		vol = exVol
	}
	if vol < 0 {
		vol = 0
	}
	if vol > 100 {
		vol = 100
	}

	cfg := loadConfig()
	if cli.Server == "" && !cli.Random && !cli.Voice && cfg.LastServer == "" {
		fmt.Fprintln(os.Stderr, "shortwave: no server address given (or use --random)")
		fmt.Fprintln(os.Stderr, "try 'shortwave --help'")
		os.Exit(1)
	}

	client, server, err := dialServer(&cli, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shortwave:", err)
		os.Exit(1)
	}
	cfg.LastServer = server
	cfg.save()
	defer client.Close()

	if cli.Test {
		os.Exit(runTest(client, cli))
	}
	if cli.Scan {
		os.Exit(runScan(client, cli))
	}

	// Resolve starting mode and frequency (a random freq needs the server's
	// reported range, so it's chosen after we learn the sample rate).
	mode := "am"
	switch {
	case cli.Mode != nil:
		mode = *cli.Mode
	case cli.Example != nil:
		mode = exMode
	case cfg.LastMode != "":
		mode = cfg.LastMode
	}
	freq := defaultFreq
	switch {
	case cli.Freq != nil:
		freq = *cli.Freq
	case cli.Example != nil:
		freq = exFreq
	case cfg.LastFreq > 0:
		freq = cfg.LastFreq
	}
	if !isMode(mode) {
		fmt.Fprintf(os.Stderr, "shortwave: unknown mode %q, using am\n", mode)
		mode = "am"
	}

	a := &app{
		freq: freq,
		mode: mode,
		vol:  vol,
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

	// With --random/--voice, pick a valid frequency within this receiver's range.
	a.mu.Lock()
	if cli.Random || cli.Voice {
		if cli.Voice {
			a.freq, a.mode = pickVoiceFreq(client.MaxKhz)
		} else {
			a.freq, a.mode = pickRandomFreq(client.MaxKhz)
		}
		if cli.Mode != nil {
			a.mode = *cli.Mode
		}
		fmt.Printf("random: %s at %g kHz %s\n", server, a.freq, a.mode)
	}
	cfg.LastFreq = a.freq
	cfg.LastMode = a.mode
	a.mu.Unlock()
	cfg.save()

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

// scanHit is one measured peak during a --scan sweep.
type scanHit struct {
	freq float64
	sig  int
}

const (
	scanDwell       = 400 * time.Millisecond
	scanStepDefault = 5.0 // kHz, multi-band scan
	scanStepBand    = 2.5 // kHz, single --band scan
)

// scanSegments returns the (mode, lo, hi) ranges to sweep, plus the step.
func scanSegments(cli CLI) ([]swBand, float64) {
	if cli.Band != "" {
		parts := strings.SplitN(cli.Band, "-", 2)
		if len(parts) == 2 {
			lo, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
			hi, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
			if err1 == nil && err2 == nil && hi > lo && lo >= 0 {
				return []swBand{{lo: lo, hi: hi, mode: "am"}}, scanStepBand
			}
		}
		fmt.Fprintln(os.Stderr, "shortwave: bad --band value (want lo-hi in kHz, e.g. 7100-7300)")
		os.Exit(1)
	}
	return voiceBands, scanStepDefault
}

// runScan sweeps a band, measuring the signal meter at each step, then prints
// the strongest signals. No audio output is needed.
func runScan(client *KiwiClient, cli CLI) int {
	rateCh := make(chan int, 1)
	errCh := make(chan error, 1)
	client.OnRate = func(r int) {
		select {
		case rateCh <- r:
		default:
		}
	}
	client.OnError = func(e error) {
		select {
		case errCh <- e:
		default:
		}
	}

	var sigMu sync.Mutex
	var sigBuf []int
	client.OnSmeter = func(s int) {
		sigMu.Lock()
		sigBuf = append(sigBuf, s)
		sigMu.Unlock()
	}

	client.Start()
	if err := client.Auth(); err != nil {
		fmt.Fprintln(os.Stderr, "shortwave:", err)
		return 1
	}

	var rate int
	select {
	case rate = <-rateCh:
	case err := <-errCh:
		fmt.Fprintln(os.Stderr, "shortwave:", err)
		return 1
	case <-time.After(30 * time.Second):
		fmt.Fprintln(os.Stderr, "shortwave: timed out waiting for the server")
		return 1
	}
	client.SetAgc()
	client.ArOk(rate, int(outRate))

	segments, step := scanSegments(cli)
	total := expectedSteps(segments, step)
	fmt.Printf("scanning for active signals ... (step %.1f kHz, ~%.0fs total)\n", step,
		float64(scanDwell.Milliseconds())/1000*float64(total))

	start := time.Now()
	var hits []scanHit
	for _, seg := range segments {
		mode := seg.mode
		if cli.Mode != nil {
			mode = *cli.Mode
		}
		fmt.Printf("\n%s band %.0f-%.0f kHz:\n", strings.ToUpper(mode), seg.lo, seg.hi)
		steps := 0
		for f := seg.lo; f <= seg.hi; f += step {
			if err := client.Tune(mode, f); err != nil {
				fmt.Fprintln(os.Stderr, "shortwave:", err)
				return 1
			}
			sigMu.Lock()
			sigBuf = sigBuf[:0]
			sigMu.Unlock()
			time.Sleep(scanDwell)

			sigMu.Lock()
			peak := 0
			for _, s := range sigBuf {
				if s > peak {
					peak = s
				}
			}
			sigMu.Unlock()

			hits = append(hits, scanHit{freq: f, sig: peak})
			steps++
			if steps%50 == 0 {
				fmt.Printf("\r  ... %6.1f kHz", f)
			}
		}
		fmt.Println()
	}

	// Sort by signal strength, show the top 30.
	sort.Slice(hits, func(i, j int) bool { return hits[i].sig > hits[j].sig })
	fmt.Printf("\nstrongest signals (of %d frequencies scanned in %s):\n", len(hits),
		time.Since(start).Round(time.Second))
	shown := 0
	for _, h := range hits {
		if h.sig <= 0 || shown >= 30 {
			continue
		}
		dBm := float64(h.sig)/10 - 127
		fmt.Printf("  %7.1f kHz  sig=%4d  ~%6.1f dBm\n", h.freq, h.sig, dBm)
		shown++
	}
	return 0
}

func expectedSteps(segments []swBand, step float64) int {
	n := 0
	for _, s := range segments {
		n += int((s.hi - s.lo) / step)
	}
	return n
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
