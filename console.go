package main

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/fatih/color"

	"github.com/pg-tools/pgcompare/internal/pgcompare"
)

const consoleWidth = 68

var (
	colArrow   = color.New(color.FgCyan, color.Bold)
	colOK      = color.New(color.FgGreen, color.Bold)
	colFail    = color.New(color.FgRed, color.Bold)
	colTime    = color.New(color.FgHiBlack)
	colRule    = color.New(color.FgHiBlack)
	colGood    = color.New(color.FgGreen)
	colNeutral = color.New(color.FgYellow)
	colBad     = color.New(color.FgRed)
	colDim     = color.New(color.FgHiBlack)
)

type phase struct {
	name    string
	started time.Time
	out     io.Writer
}

func startPhase(name string) *phase {
	out := os.Stderr
	colArrow.Fprint(out, "▶ ")
	fmt.Fprintln(out, name)
	return &phase{name: name, started: time.Now(), out: out}
}

func (p *phase) Done() time.Duration {
	elapsed := time.Since(p.started)
	colOK.Fprint(p.out, "✓ ")
	pad := consoleWidth - 2 - len(p.name)
	if pad < 1 {
		pad = 1
	}
	fmt.Fprintf(p.out, "%s%s", p.name, spaces(pad))
	colTime.Fprintf(p.out, "[%s]\n", formatDuration(elapsed))
	fmt.Fprintln(p.out)
	return elapsed
}

func (p *phase) Fail(err error) time.Duration {
	elapsed := time.Since(p.started)
	colFail.Fprint(p.out, "✗ ")
	pad := consoleWidth - 2 - len(p.name)
	if pad < 1 {
		pad = 1
	}
	fmt.Fprintf(p.out, "%s%s", p.name, spaces(pad))
	colTime.Fprintf(p.out, "[%s]\n", formatDuration(elapsed))
	if err != nil {
		colFail.Fprintf(p.out, "  %s\n", err.Error())
	}
	fmt.Fprintln(p.out)
	return elapsed
}

func printHeader(title string) {
	out := os.Stderr
	rule := ""
	padLen := consoleWidth - len(title) - 8
	if padLen < 3 {
		padLen = 3
	}
	for i := 0; i < padLen; i++ {
		rule += "━"
	}
	colRule.Fprintf(out, "━━━ %s ", title)
	colRule.Fprintln(out, rule)
}

func printSummary(data pgcompare.ReportData, outPath string, total time.Duration) {
	out := os.Stderr
	fmt.Fprintln(out)
	printHeader("Summary")

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	colDim.Fprintln(tw, "  Query\tBefore P95\tAfter P95\tSpeedup\t")
	for i, s := range data.Before.Stats {
		after := data.After.Stats[i]
		sp := 0.0
		if i < len(data.Speedups) {
			sp = data.Speedups[i]
		}
		marker, markColor := speedupMarker(sp)
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s  %s\t\n",
			s.QueryName,
			formatLatency(s.P95),
			formatLatency(after.P95),
			markColor.Sprintf("%.1f×", sp),
			markColor.Sprint(marker),
		)
	}
	tw.Flush()

	fmt.Fprintln(out)
	colDim.Fprintf(out, "  Total: %s · Report: ", formatDuration(total))
	fmt.Fprintln(out, outPath)
}

func speedupMarker(sp float64) (string, *color.Color) {
	switch {
	case sp >= 1.1:
		return "✓", colGood
	case sp >= 0.9:
		return "~", colNeutral
	default:
		return "✗", colBad
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func formatLatency(d time.Duration) string {
	us := d.Microseconds()
	if us < 1000 {
		return fmt.Sprintf("%dµs", us)
	}
	return fmt.Sprintf("%.1fms", float64(us)/1000)
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}
