// Package cli parses flags and orchestrates one shortwave session:
// dial, handshake, live listening, scan, or protocol test.
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"time"

	"github.com/alecthomas/kong"

	"github.com/CHE3MZ/shortwave/internal/app"
	"github.com/CHE3MZ/shortwave/internal/audio"
	"github.com/CHE3MZ/shortwave/internal/bands"
	"github.com/CHE3MZ/shortwave/internal/config"
	"github.com/CHE3MZ/shortwave/internal/kiwi"
	"github.com/CHE3MZ/shortwave/internal/ui"
	"github.com/CHE3MZ/shortwave/internal/version"
)

const (
	// DefaultFreq is the startup frequency in kHz (~10 MHz SW band).
	DefaultFreq = 10000.0
)

// CLI defines the command-line surface. Kong derives --help from these tags.
type CLI struct {
	Random  bool     `kong:"optional,name='random',help='Pick a random public KiwiSDR server and a random valid frequency.'"`
	Voice   bool     `kong:"optional,name='voice',help='Tune to a random ham SSB phone frequency where people talk.'"`
	Example *int     `kong:"optional,name='example',help='Listen to a preset: 1 = 7222 kHz LSB @ vol 25, 2 = 1010 kHz LSB @ vol 25, 3 = 1030 kHz LSB @ vol 25.'"`
	Scan    bool     `kong:"optional,name='scan',help='Sweep a band and list the strongest signals (no audio).'"`
	Band    string   `kong:"optional,name='band',help='With --scan: range to sweep, e.g. --band 7100-7300.'"`
	Mode    *string  `kong:"optional,short='m',help='Demodulation mode: am, amn, usb, usn, lsb, lsn, cw, cwn, nbfm, nnfm, sam (default am; auto-selected otherwise).'"`
	Volume  *int     `kong:"optional,short='v',help='Volume 0-100 (default 80).'"`
	NoColor bool     `kong:"optional,name='no-color',help='Disable colored output (also honors NO_COLOR).'"`
	Version bool     `kong:"optional,name='version',help='Show version and exit.'"`
	Test    bool     `kong:"optional,name='test',help='Connect, verify the protocol, then exit (no audio).'"`
	Server  string   `kong:"arg,optional,help='KiwiSDR server, e.g. kiwisdr.ucsd.edu:8073 (defaults to the last one used).'"`
	Freq    *float64 `kong:"optional,short='f',help='Start frequency in kHz (default 10000; random with --random/--voice).'"`
}

// NormalizeArgs lets "--example" work bare as well as with a value:
// "--example" (no value) becomes "--example=1"; "--example 2" is left alone.
func NormalizeArgs(args []string) []string {
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

// examplePreset resolves --example N to (freq, mode, vol).
func examplePreset(n int) (freq float64, mode string, vol int, ok bool) {
	switch n {
	case 1:
		return 7222, "lsb", 25, true
	case 2:
		return 1010, "lsb", 25, true
	case 3:
		return 1030, "lsb", 25, true
	default:
		return 0, "", 0, false
	}
}

func clampVolume(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// Run parses the command line and runs the session. It returns the exit code.
func Run() int {
	os.Args = append([]string{os.Args[0]}, NormalizeArgs(os.Args[1:])...)
	var cli CLI
	kong.Parse(&cli,
		kong.Name("shortwave"),
		kong.Description("Listen to shortwave radio over the internet via a remote KiwiSDR.\n\n"+
			"Usage:\n  shortwave [<server>] [flags]\n\n"+
			"While listening:  q quit, +/- step 5 kHz, 1/2 step 1 kHz, <number> tune to kHz,\n"+
			"  m <mode> change mode, v <0-100> volume, ? status\n"+
			"\n--example presets:\n  --example 1   ->   -f 7222 -m lsb -v 25\n"+
			"  --example 2   ->   -f 1010 -m lsb -v 25\n"+
			"  --example 3   ->   -f 1030 -m lsb -v 25\n"+
			"\nExamples:\n  shortwave alg.twrmon.net:8073 -f 9650 -m am\n"+
			"  shortwave --random --volume 50"),
		kong.UsageOnError(),
	)

	if cli.NoColor || os.Getenv("NO_COLOR") != "" {
		ui.SetNoColor(true)
	}

	if cli.Version {
		fmt.Printf("shortwave %s\n", version.Version)
		return 0
	}
	if cli.Example != nil {
		if _, _, _, ok := examplePreset(*cli.Example); !ok {
			ui.PrintErr("unknown --example value %d (valid: 1, 2, 3)", *cli.Example)
			return 1
		}
	}

	// --example presets.
	var exFreq float64
	exMode := ""
	exVol := 0
	if cli.Example != nil {
		var ok bool
		exFreq, exMode, exVol, ok = examplePreset(*cli.Example)
		if !ok {
			ui.PrintErr("unknown --example value %d (valid: 1, 2, 3)", *cli.Example)
			return 1
		}
	}

	vol := 80
	if cli.Volume != nil {
		vol = *cli.Volume
	} else if cli.Example != nil {
		vol = exVol
	}
	vol = clampVolume(vol)

	cfg := config.Load()
	if cli.Server == "" && !cli.Random && !cli.Voice && cfg.LastServer == "" {
		ui.PrintErr("no server address given (or use --random)")
		fmt.Fprintln(os.Stderr, "try 'shortwave --help'")
		return 1
	}

	client, server, err := app.DialServer(cli.Server, cli.Random, cfg.LastServer)
	if err != nil {
		ui.PrintErr("%v", err)
		return 1
	}
	cfg.LastServer = server
	if err := cfg.Save(); err != nil {
		fmt.Println(ui.Warn("could not save config: %v", err))
	}
	defer func() { _ = client.Close() }()

	if cli.Test {
		mode := "am"
		if cli.Mode != nil {
			mode = *cli.Mode
		}
		freq := DefaultFreq
		if cli.Freq != nil {
			freq = *cli.Freq
		}
		return app.RunTest(client, cli.Server, mode, freq)
	}
	if cli.Scan {
		return app.RunScan(client, cli.Band, cli.Mode)
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
	freq := DefaultFreq
	switch {
	case cli.Freq != nil:
		freq = *cli.Freq
	case cli.Example != nil:
		freq = exFreq
	case cfg.LastFreq > 0:
		freq = cfg.LastFreq
	}
	if !kiwi.IsMode(mode) {
		fmt.Println(ui.Warn("unknown mode %q, using am", mode))
		mode = "am"
	}

	session := app.New(freq, mode, vol)
	session.Attach(client, nil)

	rateCh := make(chan int, 4)
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
		session.SetSmeter(s)
	}
	client.OnAudio = func(payload []byte, flags byte) {
		// Player is attached after the rate handshake; drop early frames.
		session.Push(client.DecodeAudio(payload, flags))
	}

	client.Start()
	if err := client.Auth(); err != nil {
		ui.PrintErr("%v", err)
		return 1
	}
	session.SetConnected()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopSignals(cancel)

	// Keepalive every 5s so the server never times us out.
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := client.Send("SET keepalive"); err != nil {
					return
				}
			}
		}
	}()

	// Wait for the audio sample rate (also confirms auth succeeded).
	var rate int
	select {
	case rate = <-rateCh:
	case err := <-errCh:
		ui.PrintErr("%v", err)
		return 1
	case <-time.After(30 * time.Second):
		ui.PrintErr("timed out waiting for the server")
		return 1
	}

	player, err := audio.NewPlayer(rate)
	if err != nil {
		ui.PrintErr("audio output failed: %v", err)
		return 1
	}
	defer player.Close()
	session.Attach(client, player)
	session.SetRate(rate)
	player.SetVolume(vol)

	// With --random/--voice, pick a valid frequency within this receiver's range.
	if cli.Random || cli.Voice {
		newFreq, newMode := bands.PickRandomFreq(client.MaxKhz)
		if cli.Voice {
			newFreq, newMode = bands.PickVoiceFreq(client.MaxKhz)
		}
		if cli.Mode != nil {
			newMode = *cli.Mode
		}
		session.SetFreqMode(newFreq, newMode)
		fmt.Println(ui.Info("random: %s at %g kHz %s", server, newFreq, newMode))
	}
	f, m, _, _, _, _ := session.Snapshot()
	cfg.LastFreq = f
	cfg.LastMode = m
	if err := cfg.Save(); err != nil {
		fmt.Println(ui.Warn("could not save config: %v", err))
	}

	freq, mode = session.FreqMode()
	if err := client.Tune(mode, freq); err != nil {
		ui.PrintErr("%v", err)
		return 1
	}
	if err := client.SetAgc(); err != nil {
		ui.PrintErr("agc: %v", err)
		return 1
	}
	if err := client.ArOk(rate, int(audio.OutRate)); err != nil {
		ui.PrintErr("audio-rate handshake: %v", err)
		return 1
	}

	// Signal meter ticker.
	statusDone := make(chan struct{})
	go func() {
		t := time.NewTicker(1 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				fmt.Printf("\r%s", session.StatusText())
			case <-statusDone:
				return
			}
		}
	}()
	defer close(statusDone)

	// Watch for connection errors in the background. Exit so a dropped
	// connection doesn't leave us hanging on a stdin read.
	watchDone := make(chan struct{})
	go func() {
		select {
		case err := <-errCh:
			fmt.Fprintf(os.Stderr, "\n%s\n", ui.Error("connection lost: %v", err))
			time.Sleep(300 * time.Millisecond)
			os.Exit(1)
		case <-watchDone:
		}
	}()
	defer close(watchDone)

	fmt.Println(ui.Banner(version.Version))
	fmt.Println(ui.Success("listening on %s", server))
	fmt.Println(session.StatusText())
	fmt.Println(ui.CommandsBox())
	session.Interactive(ctx)
	return 0
}

// stopSignals cancels the session context on Ctrl-C.
func stopSignals(cancel context.CancelFunc) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	go func() {
		<-ch
		cancel()
	}()
}
