// Package ui renders the pretty terminal output: banner, live status line,
// scan tables, and colored info/success/error messages.
//
// All helpers respect SetNoColor(true), which makes them return plain text
// for pipes, logs, and --no-color runs.
package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var noColor bool

// SetNoColor disables styled output and returns plain text instead.
func SetNoColor(v bool) { noColor = v }

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	freqStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	modeStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("13"))
	volStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	sigStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	okStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	errStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
	warnStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	infoStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	boxStyle   = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("8")).
			Padding(0, 1)
)

// Banner returns the startup header.
func Banner(version string) string {
	if noColor {
		return fmt.Sprintf("SHORTWAVE v%s — live shortwave radio via KiwiSDR", version)
	}
	head := titleStyle.Render("S H O R T W A V E") + "  " + dimStyle.Render("v"+version)
	sub := dimStyle.Render("live shortwave radio through public KiwiSDR receivers")
	rule := dimStyle.Render(strings.Repeat("─", 52))
	return fmt.Sprintf("%s\n%s\n%s", head, sub, rule)
}

// SignalDBM converts the raw 16-bit meter value to an approximate dBm.
func SignalDBM(sig int) float64 { return float64(sig)/10 - 127 }

// SignalBar renders a 10-cell meter: filled cells scale with sig.
func SignalBar(sig int) string {
	const width = 10
	filled := sig * width / 1200
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	if noColor {
		return "[" + bar + "]"
	}
	switch {
	case sig >= 800:
		return sigStyle.Render(bar)
	case sig >= 400:
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10")).Render(bar)
	default:
		return dimStyle.Render(bar)
	}
}

// Status renders the one-line live readout shown every second.
func Status(freq float64, mode string, vol, sig, rate int, connected bool) string {
	dbm := SignalDBM(sig)
	var state string
	if connected {
		if noColor {
			state = "live"
		} else {
			state = okStyle.Render("● live")
		}
	} else if noColor {
		state = "connecting..."
	} else {
		state = warnStyle.Render("● connecting…")
	}
	if noColor {
		return fmt.Sprintf("freq=%7.1f kHz  mode=%-4s  vol=%3d%%  sig=%4d (~%6.1f dBm)  rate=%d Hz  %s",
			freq, mode, vol, sig, dbm, rate, state)
	}
	return fmt.Sprintf("%s %s  %s %s  %s %s  %s %s %s  %s %d Hz  %s",
		dimStyle.Render("freq"),
		freqStyle.Render(fmt.Sprintf("%7.1f kHz", freq)),
		dimStyle.Render("mode"),
		modeStyle.Render(fmt.Sprintf("%-4s", mode)),
		dimStyle.Render("vol"),
		volStyle.Render(fmt.Sprintf("%3d%%", vol)),
		dimStyle.Render("sig"),
		sigStyle.Render(fmt.Sprintf("%4d", sig)),
		dimStyle.Render(fmt.Sprintf("~%6.1f dBm", dbm)),
		dimStyle.Render("rate"),
		rate,
		state,
	)
}

// CommandsBox renders the interactive key reference.
func CommandsBox() string {
	rows := []struct{ keys, action string }{
		{"q", "quit"},
		{"+ / -", "step 5 kHz up / down"},
		{"1 / 2", "step 1 kHz up / down"},
		{"<number>", "tune to kHz (e.g. 9650)"},
		{"m <mode>", "change mode (am, lsb, usb, cw, nbfm, ...)"},
		{"v <0-100>", "volume"},
		{"?", "show status"},
	}
	var b strings.Builder
	for _, r := range rows {
		if noColor {
			fmt.Fprintf(&b, "  %-10s %s\n", r.keys, r.action)
			continue
		}
		fmt.Fprintf(&b, "  %s  %s\n",
			infoStyle.Render(fmt.Sprintf("%-10s", r.keys)),
			r.action)
	}
	body := strings.TrimRight(b.String(), "\n")
	if noColor {
		return "commands:\n" + body
	}
	return boxStyle.Render(titleStyle.Render("commands") + "\n" + body)
}

// Prompt is the interactive input prefix.
func Prompt() string {
	if noColor {
		return "shortwave> "
	}
	return infoStyle.Render("shortwave› ") //nolint:gosec // static prompt string
}

// Info returns a cyan "info" line.
func Info(format string, args ...any) string {
	msg := fmt.Sprintf(format, args...)
	if noColor {
		return "info: " + msg
	}
	return infoStyle.Render("info") + "  " + msg
}

// Success returns a green "ok" line.
func Success(format string, args ...any) string {
	msg := fmt.Sprintf(format, args...)
	if noColor {
		return "ok: " + msg
	}
	return okStyle.Render("ok") + "  " + msg
}

// Warn returns a yellow "warn" line.
func Warn(format string, args ...any) string {
	msg := fmt.Sprintf(format, args...)
	if noColor {
		return "warn: " + msg
	}
	return warnStyle.Render("warn") + "  " + msg
}

// Error returns a red "error" line (without printing; caller decides stream).
func Error(format string, args ...any) string {
	msg := fmt.Sprintf(format, args...)
	if noColor {
		return "error: " + msg
	}
	return errStyle.Render("error") + "  " + msg
}

// PrintErr writes an error line to stderr.
func PrintErr(format string, args ...any) {
	fmt.Fprintln(os.Stderr, Error(format, args...))
}

// ScanRow formats one scan hit for the results table.
func ScanRow(freq float64, sig int) string {
	dbm := SignalDBM(sig)
	bar := SignalBar(sig)
	if noColor {
		return fmt.Sprintf("  %7.1f kHz  sig=%4d  ~%6.1f dBm  %s", freq, sig, dbm, bar)
	}
	return fmt.Sprintf("  %s  %s  %s  %s",
		freqStyle.Render(fmt.Sprintf("%7.1f kHz", freq)),
		sigStyle.Render(fmt.Sprintf("sig=%4d", sig)),
		dimStyle.Render(fmt.Sprintf("~%6.1f dBm", dbm)),
		bar)
}

// ScanHeader renders a band section header.
func ScanHeader(mode string, lo, hi float64) string {
	label := fmt.Sprintf("%s band %.0f-%.0f kHz", strings.ToUpper(mode), lo, hi)
	if noColor {
		return label + ":"
	}
	return titleStyle.Render("▸ "+label) //nolint:gosec // static decoration
}
