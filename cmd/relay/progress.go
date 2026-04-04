package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const renderInterval = 80 * time.Millisecond

// progressWriter wraps an io.Writer and renders a progress bar to stderr on
// each Write call (throttled by renderInterval to avoid excessive redraws).
type progressWriter struct {
	dest        io.Writer
	filename    string
	total       int64 // total bytes remaining for this transfer (0 = unknown)
	offset      int64 // bytes already on disk before this session (resume offset)
	written     int64 // bytes written in this session
	startTime   time.Time
	lastRender  time.Time
	lastLineLen int // width of last rendered line; used to erase stale chars
	done        bool
}

func newProgressWriter(dest io.Writer, filename string, offset, total int64) *progressWriter {
	return &progressWriter{
		dest:      dest,
		filename:  filename,
		total:     total,
		offset:    offset,
		startTime: time.Now(),
	}
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n, err := pw.dest.Write(p)
	pw.written += int64(n)
	if time.Since(pw.lastRender) >= renderInterval {
		pw.render()
		pw.lastRender = time.Now()
	}
	return n, err
}

// finish prints the final completed line and moves to the next line.
func (pw *progressWriter) finish() {
	if pw.done {
		return
	}
	pw.done = true
	pw.render()
	fmt.Fprintln(os.Stderr)
}

func (pw *progressWriter) render() {
	elapsed := time.Since(pw.startTime)
	if elapsed == 0 {
		elapsed = time.Millisecond
	}

	speed := float64(pw.written) / elapsed.Seconds() // bytes/s
	transferred := pw.offset + pw.written

	// Filename column: truncate to 18 characters.
	name := pw.filename
	if len(name) > 18 {
		name = name[:15] + "..."
	}

	var line string
	if pw.total > 0 {
		pct := int(float64(pw.written) / float64(pw.total) * 100)
		if pct > 100 {
			pct = 100
		}

		const barWidth = 28
		filled := barWidth * pct / 100
		bar := strings.Repeat("=", filled)
		if filled < barWidth {
			bar += ">"
			bar += strings.Repeat(" ", barWidth-filled-1)
		}

		eta := ""
		switch {
		case pct >= 100:
			eta = "Done"
		case speed > 0:
			secs := int(float64(pw.total-pw.written)/speed) + 1
			eta = fmt.Sprintf("ETA %s", formatDuration(time.Duration(secs)*time.Second))
		}

		line = fmt.Sprintf("  %-18s [%s] %3d%%  %8s/s  %-12s",
			name, bar, pct, formatBytes(int64(speed)), eta)
	} else {
		// Unknown total - show spinner + transferred + speed.
		frames := []string{"|", "/", "-", "\\"}
		spin := frames[int(elapsed.Milliseconds()/200)%len(frames)]
		line = fmt.Sprintf("  %-18s  %s  %8s  @ %s/s",
			name, spin, formatBytes(transferred), formatBytes(int64(speed)))
	}

	// Pad to last line length so stale characters are fully overwritten.
	if len(line) < pw.lastLineLen {
		line += strings.Repeat(" ", pw.lastLineLen-len(line))
	}
	pw.lastLineLen = len(line)

	fmt.Fprint(os.Stderr, "\r"+line)
}

// formatBytes returns a human-readable byte count (e.g. "1.5 MB").
func formatBytes(b int64) string {
	if b < 0 {
		b = 0
	}
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// formatDuration returns a compact duration string (e.g. "1:23" or "0:05").
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// isTerminal reports whether f is connected to an interactive terminal.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
