package app

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CHE3MZ/shortwave/internal/audio"
	"github.com/CHE3MZ/shortwave/internal/bands"
	"github.com/CHE3MZ/shortwave/internal/kiwi"
	"github.com/CHE3MZ/shortwave/internal/ui"
)

// ScanHit is one measured peak during a --scan sweep.
type ScanHit struct {
	Freq float64
	Sig  int
}

const (
	// ScanDwell is how long to listen per step before reading the meter.
	ScanDwell = 400 * time.Millisecond
	// ScanStepDefault is the step for the multi-band sweep.
	ScanStepDefault = 5.0 // kHz
	// ScanStepBand is the finer step for a single --band sweep.
	ScanStepBand = 2.5 // kHz
)

// ScanSegments returns the (mode, lo, hi) ranges to sweep, plus the step.
// A bandRange like "7100-7300" scopes the sweep; empty scopes the voice bands.
func ScanSegments(bandRange string) ([]bands.Band, float64, error) {
	if bandRange != "" {
		parts := strings.SplitN(bandRange, "-", 2)
		if len(parts) == 2 {
			lo, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
			hi, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
			if err1 == nil && err2 == nil && hi > lo && lo >= 0 {
				return []bands.Band{{Lo: lo, Hi: hi, Mode: "am"}}, ScanStepBand, nil
			}
		}
		return nil, 0, fmt.Errorf("bad --band value %q (want lo-hi in kHz, e.g. 7100-7300)", bandRange)
	}
	return bands.VoiceBands, ScanStepDefault, nil
}

// ExpectedSteps counts the tune steps for a progress estimate.
func ExpectedSteps(segments []bands.Band, step float64) int {
	n := 0
	for _, s := range segments {
		n += int((s.Hi - s.Lo) / step)
	}
	return n
}

// RunScan sweeps a band, measuring the signal meter at each step, then prints
// the strongest signals. No audio output is needed.
func RunScan(client *kiwi.Client, bandRange string, modeOverride *string) int {
	rateCh := make(chan int, 4)
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
		ui.PrintErr("%v", err)
		return 1
	}

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
	if err := client.SetAgc(); err != nil {
		ui.PrintErr("agc: %v", err)
		return 1
	}
	if err := client.ArOk(rate, int(audio.OutRate)); err != nil {
		ui.PrintErr("audio-rate handshake: %v", err)
		return 1
	}

	segments, step, err := ScanSegments(bandRange)
	if err != nil {
		ui.PrintErr("%v", err)
		return 1
	}
	total := ExpectedSteps(segments, step)
	fmt.Printf("scanning for active signals ... (step %.1f kHz, ~%.0fs total)\n", step,
		float64(ScanDwell.Milliseconds())/1000*float64(total))

	start := time.Now()
	var hits []ScanHit
	for _, seg := range segments {
		mode := seg.Mode
		if modeOverride != nil {
			mode = *modeOverride
		}
		fmt.Println()
		fmt.Println(ui.ScanHeader(mode, seg.Lo, seg.Hi))
		steps := 0
		for f := seg.Lo; f <= seg.Hi; f += step {
			if err := client.Tune(mode, f); err != nil {
				ui.PrintErr("%v", err)
				return 1
			}
			sigMu.Lock()
			sigBuf = sigBuf[:0]
			sigMu.Unlock()
			time.Sleep(ScanDwell)

			sigMu.Lock()
			peak := 0
			for _, s := range sigBuf {
				if s > peak {
					peak = s
				}
			}
			sigMu.Unlock()

			hits = append(hits, ScanHit{Freq: f, Sig: peak})
			steps++
			if steps%50 == 0 {
				fmt.Fprintf(os.Stderr, "\r  ... %6.1f kHz", f)
			}
		}
		fmt.Fprintln(os.Stderr)
	}

	// Sort by signal strength, show the top 30.
	sort.Slice(hits, func(i, j int) bool { return hits[i].Sig > hits[j].Sig })
	fmt.Printf("\nstrongest signals (of %d frequencies scanned in %s):\n", len(hits),
		time.Since(start).Round(time.Second))
	shown := 0
	for _, h := range hits {
		if h.Sig <= 0 || shown >= 30 {
			continue
		}
		fmt.Println(ui.ScanRow(h.Freq, h.Sig))
		shown++
	}
	if shown == 0 {
		fmt.Println(ui.Warn("no signals above the noise floor — try another band or time of day"))
	}
	return 0
}
