// Package app holds the shared listening session: connection state,
// tuning, the interactive prompt, and server selection.
package app

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/CHE3MZ/shortwave/internal/audio"
	"github.com/CHE3MZ/shortwave/internal/kiwi"
	"github.com/CHE3MZ/shortwave/internal/ui"
)

// StepBig is the +/- tuning step in kHz.
const StepBig = 5.0

// DefaultServerPool is the curated set of public KiwiSDR receivers used by
// --random. Extend freely; unreachable servers are skipped.
var DefaultServerPool = []string{
	"alg.twrmon.net:8073",
	"kiwisdr.njctech.com:8073",
	"kiwisdr.areg.org.au:8073",
	"22033.proxy.kiwisdr.com:8073",
}

// MaxRandomTry bounds how many pool servers --random dials.
const MaxRandomTry = 3

// App is the live listening session shared by the prompt and the meter.
type App struct {
	mu        sync.Mutex
	freq      float64
	mode      string
	vol       int
	smeter    int
	rate      int
	client    *kiwi.Client
	player    *audio.Player
	connected bool
}

// New creates a session with the starting frequency, mode, and volume.
func New(freq float64, mode string, vol int) *App {
	return &App{freq: freq, mode: mode, vol: vol}
}

// Attach wires a connected client and player to the session.
func (a *App) Attach(client *kiwi.Client, player *audio.Player) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.client = client
	a.player = player
}

// SetConnected marks the handshake complete so the status shows live.
func (a *App) SetConnected() {
	a.mu.Lock()
	a.connected = true
	a.mu.Unlock()
}

// SetSmeter records the latest signal meter value.
func (a *App) SetSmeter(s int) {
	a.mu.Lock()
	a.smeter = s
	a.mu.Unlock()
}

// SetRate records the negotiated audio sample rate.
func (a *App) SetRate(rate int) {
	a.mu.Lock()
	a.rate = rate
	a.mu.Unlock()
}

// Snapshot returns the values needed for one status line.
func (a *App) Snapshot() (freq float64, mode string, vol, smeter, rate int, connected bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.freq, a.mode, a.vol, a.smeter, a.rate, a.connected
}

// SetFreqMode replaces the tuned frequency and mode (used for --random).
func (a *App) SetFreqMode(freq float64, mode string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.freq = freq
	a.mode = mode
}

// FreqMode returns the current frequency and mode.
func (a *App) FreqMode() (float64, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.freq, a.mode
}

// Push feeds decoded samples to the attached player, if any.
func (a *App) Push(samples []float64) {
	a.mu.Lock()
	player := a.player
	a.mu.Unlock()
	if player != nil {
		player.Push(samples)
	}
}

// StatusText renders the pretty one-line readout.
func (a *App) StatusText() string {
	freq, mode, vol, smeter, rate, connected := a.Snapshot()
	return ui.Status(freq, mode, vol, smeter, rate, connected)
}

// DialServer connects to the requested server, a random pool server (when
// random is set, avoiding the previous one), or errors when none is known.
func DialServer(explicit string, random bool, lastServer string) (*kiwi.Client, string, error) {
	// Explicit server always wins.
	if explicit != "" {
		c, err := kiwi.Dial(explicit)
		return c, kiwi.NormalizeServer(explicit), err
	}

	// --random: try random pool servers, skipping the previous one.
	if random {
		filtered := make([]string, 0, len(DefaultServerPool))
		for _, s := range DefaultServerPool {
			if s != lastServer {
				filtered = append(filtered, s)
			}
		}
		if len(filtered) == 0 {
			filtered = DefaultServerPool
		}
		var lastErr error
		tried := 0
		for _, srv := range shuffled(filtered) {
			if tried >= MaxRandomTry {
				break
			}
			tried++
			fmt.Println(ui.Info("trying random server %s ...", srv))
			c, err := kiwi.Dial(srv)
			if err == nil {
				fmt.Println(ui.Success("connected to %s", srv))
				return c, srv, nil
			}
			lastErr = err
			fmt.Println(ui.Warn("%s is unreachable: %v", srv, err))
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("server pool is empty")
		}
		return nil, "", fmt.Errorf("no public server could be reached: %w", lastErr)
	}

	return nil, "", fmt.Errorf("no server address given (use --random, an address, or run once with both)")
}

// Tune retunes the receiver, clamping to the valid range.
func (a *App) Tune(f float64) {
	if f < 0 {
		f = 0
	}
	a.mu.Lock()
	if a.client != nil && a.client.MaxKhz > 0 && f > a.client.MaxKhz {
		f = a.client.MaxKhz
	}
	a.freq = f
	mode := a.mode
	client := a.client
	a.mu.Unlock()
	if client == nil {
		return
	}
	if err := client.Tune(mode, f); err != nil {
		ui.PrintErr("tune: %v", err)
	}
}

// Interactive reads tuning commands until quit or EOF.
func (a *App) Interactive(ctx context.Context) {
	fmt.Print(ui.Prompt())
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
			fmt.Println(a.StatusText())
		case cmd == "+" || cmd == "up":
			a.Tune(a.currentFreq() + StepBig)
		case cmd == "-" || cmd == "down":
			a.Tune(a.currentFreq() - StepBig)
		case cmd == "1":
			a.Tune(a.currentFreq() + 1)
		case cmd == "2":
			a.Tune(a.currentFreq() - 1)
		case strings.HasPrefix(cmd, "m "):
			m := strings.TrimSpace(cmd[2:])
			if kiwi.IsMode(m) {
				a.mu.Lock()
				a.mode = m
				a.mu.Unlock()
				a.Tune(a.currentFreq())
				fmt.Println(ui.Success("mode → %s", m))
			} else {
				fmt.Println(ui.Error("unknown mode %q (valid: %s)", m, strings.Join(kiwi.ValidModes, ", ")))
			}
		case strings.HasPrefix(cmd, "v "):
			if n, err := strconv.Atoi(strings.TrimSpace(cmd[2:])); err == nil && n >= 0 && n <= 100 {
				a.mu.Lock()
				a.vol = n
				player := a.player
				a.mu.Unlock()
				if player != nil {
					player.SetVolume(n)
				}
				fmt.Println(ui.Success("volume → %d%%", n))
			} else {
				fmt.Println(ui.Error("volume must be 0-100"))
			}
		default:
			if f, err := strconv.ParseFloat(cmd, 64); err == nil {
				a.Tune(f)
			} else {
				fmt.Println(ui.Error("unknown command %q (try '?')", cmd))
			}
		}
		fmt.Print(ui.Prompt())
	}
}

func (a *App) currentFreq() float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.freq
}
