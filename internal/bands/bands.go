// Package bands holds the curated shortwave band tables and the random
// frequency pickers used by --random and --voice.
package bands

import (
	"math"
	"math/rand"
)

// Band is a known-active shortwave band with a sensible default mode.
type Band struct {
	Lo   float64 // kHz, band bottom
	Hi   float64 // kHz, band top
	Mode string  // default demodulation mode for the band
}

// SWBands are broadcast and ham bands with regular activity.
var SWBands = []Band{
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

// VoiceBands are ham SSB phone bands where live human speech is active daily.
var VoiceBands = []Band{
	{3600, 4000, "lsb"},   // 80m
	{7125, 7300, "lsb"},   // 40m (best in the evening)
	{14150, 14350, "usb"}, // 20m
	{18110, 18168, "usb"}, // 17m
	{21300, 21450, "usb"}, // 15m
	{28300, 29700, "usb"}, // 10m
}

// PickFromBands returns a random frequency (kHz) from the given bands, clamped
// to the receiver's tunable range so the resulting channel is always valid.
func PickFromBands(bandList []Band, maxKhz float64) (freq float64, mode string) {
	if maxKhz <= 0 {
		maxKhz = 30000
	}
	for i := 0; i < 32; i++ {
		b := bandList[rand.Intn(len(bandList))] // #nosec G404 -- non-security tuning jitter
		if b.Lo >= maxKhz {
			continue
		}
		hi := b.Hi
		if hi > maxKhz {
			hi = maxKhz
		}
		if hi <= b.Lo {
			continue
		}
		f := b.Lo + rand.Float64()*(hi-b.Lo) // #nosec G404 -- non-security tuning jitter
		return math.Round(f*10) / 10, b.Mode
	}
	f := rand.Float64() * maxKhz // #nosec G404 -- non-security tuning jitter
	return math.Round(f*10) / 10, "am"
}

// PickRandomFreq picks a random frequency on a general shortwave band.
func PickRandomFreq(maxKhz float64) (float64, string) { return PickFromBands(SWBands, maxKhz) }

// PickVoiceFreq picks a random ham SSB phone frequency, where live human
// speech is most likely.
func PickVoiceFreq(maxKhz float64) (float64, string) { return PickFromBands(VoiceBands, maxKhz) }
