package app

import (
	"fmt"
	"os"
	"time"

	"github.com/CHE3MZ/shortwave/internal/audio"
	"github.com/CHE3MZ/shortwave/internal/kiwi"
	"github.com/CHE3MZ/shortwave/internal/ui"
)

// RunTest connects, authenticates, tunes, and verifies audio frames arrive,
// then exits. It returns the process exit code.
func RunTest(client *kiwi.Client, server, mode string, freq float64) int {
	msgs := 0
	frames := 0
	comp := 0
	badp := ""
	errCh := make(chan error, 4)
	rateCh := make(chan int, 4)

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
		select {
		case rateCh <- r:
		default:
		}
	}
	client.OnSmeter = func(_ int) {}
	client.OnAudio = func(_ []byte, flags byte) {
		frames++
		if flags&kiwi.FlagCompressed != 0 {
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
		fmt.Fprintln(os.Stderr, ui.Error("auth: %v", err))
		return 1
	}

	// Wait for the sample rate, then send the full startup command set.
	var gotRate int
	select {
	case r := <-rateCh:
		gotRate = r
		if err := client.Tune(mode, freq); err != nil {
			fmt.Fprintln(os.Stderr, ui.Error("tune: %v", err))
			return 1
		}
		if err := client.SetAgc(); err != nil {
			fmt.Fprintln(os.Stderr, ui.Error("agc: %v", err))
			return 1
		}
		if err := client.ArOk(r, int(audio.OutRate)); err != nil {
			fmt.Fprintln(os.Stderr, ui.Error("audio-rate handshake: %v", err))
			return 1
		}
	case err := <-errCh:
		fmt.Fprintln(os.Stderr, ui.Error("%v", err))
		return 1
	case <-time.After(30 * time.Second):
		fmt.Fprintln(os.Stderr, ui.Error("timed out waiting for sample rate"))
		return 1
	}

	fmt.Println(ui.Info("connected to %s, tuned to %g kHz %s at %d Hz ...",
		kiwi.NormalizeServer(server), freq, mode, gotRate))

	deadline := time.After(12 * time.Second)
	rate := gotRate
	for {
		select {
		case r := <-rateCh:
			rate = r
		case err := <-errCh:
			fmt.Fprintln(os.Stderr, ui.Error("%v", err))
			return 1
		case <-deadline:
			fmt.Printf("\nresult: msgs=%d frames=%d compressed=%d rate=%d", msgs, frames, comp, rate)
			if badp != "" {
				fmt.Printf(" (%s)", badp)
			}
			fmt.Println()
			if frames > 20 && rate > 0 {
				fmt.Println(ui.Success("protocol verified"))
				return 0
			}
			fmt.Println(ui.Error("not enough audio frames"))
			return 1
		}
	}
}
