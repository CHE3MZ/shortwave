package main

import (
	"sync"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/speaker"
)

// output sample rate for the audio device.
const outRate = beep.SampleRate(48000)

type Player struct {
	mu   sync.Mutex
	vol  float64
	feed chan float64
}

// NewPlayer initializes the audio output and starts pulling audio. Samples fed
// via Push are resampled from rate to the device rate.
func NewPlayer(rate int) (*Player, error) {
	if err := speaker.Init(outRate, outRate.N(100*time.Millisecond)); err != nil {
		return nil, err
	}
	p := &Player{
		vol:  0.8,
		feed: make(chan float64, outRate.N(2*time.Second)),
	}
	base := beep.StreamerFunc(func(samples [][2]float64) (int, bool) {
		p.mu.Lock()
		vol := p.vol
		p.mu.Unlock()
		for i := range samples {
			var v float64
			select {
			case v = <-p.feed:
			default:
			}
			v *= vol
			samples[i][0] = v
			samples[i][1] = v
		}
		return len(samples), true
	})
	resampled := beep.Resample(4, beep.SampleRate(rate), outRate, base)
	speaker.Play(resampled)
	return p, nil
}

// Push feeds raw mono samples (already scaled to -1..1) into the output queue.
// If the queue is full the newest samples are dropped to avoid blocking.
func (p *Player) Push(samples []float64) {
	for _, s := range samples {
		select {
		case p.feed <- s:
		default:
		}
	}
}

func (p *Player) SetVolume(pct int) {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	p.mu.Lock()
	p.vol = float64(pct) / 100.0
	p.mu.Unlock()
}

func (p *Player) Close() {
	speaker.Clear()
}
