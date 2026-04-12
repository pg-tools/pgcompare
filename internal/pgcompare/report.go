package pgcompare

import (
	_ "embed"
	"fmt"
	"html/template"
	"os"
	"strings"
	"time"
)

//go:embed templates/report.html
var reportHTML string

//go:embed templates/chart.min.js
var chartJS string

type templateData struct {
	ReportData
	ChartJS        template.JS
	LatencyMetrics []latencyMetric
}

type latencyMetric struct {
	Name   string
	Get    func(Stats) string
	GetDur func(Stats) time.Duration
}

var latencyMetrics = []latencyMetric{
	{"P50", fmtDur(func(s Stats) time.Duration { return s.P50 }), func(s Stats) time.Duration { return s.P50 }},
	{"P95", fmtDur(func(s Stats) time.Duration { return s.P95 }), func(s Stats) time.Duration { return s.P95 }},
	{"P99", fmtDur(func(s Stats) time.Duration { return s.P99 }), func(s Stats) time.Duration { return s.P99 }},
	{"Min", fmtDur(func(s Stats) time.Duration { return s.Min }), func(s Stats) time.Duration { return s.Min }},
	{"Max", fmtDur(func(s Stats) time.Duration { return s.Max }), func(s Stats) time.Duration { return s.Max }},
	{"Mean", fmtDur(func(s Stats) time.Duration { return s.Mean }), func(s Stats) time.Duration { return s.Mean }},
}

func fmtDur(fn func(Stats) time.Duration) func(Stats) string {
	return func(s Stats) string {
		return fn(s).String()
	}
}

func renderPlan(node *PlanNode) string {
	if node == nil {
		return "(no plan)"
	}
	var b strings.Builder
	renderPlanNode(&b, node, 0)
	return b.String()
}

func renderPlanNode(b *strings.Builder, node *PlanNode, depth int) {
	indent := strings.Repeat("  ", depth)
	fmt.Fprintf(b, "%s-> %s", indent, node.NodeType)
	if node.RelationName != "" {
		fmt.Fprintf(b, " on %s", node.RelationName)
	}
	if node.IndexName != "" {
		fmt.Fprintf(b, " using %s", node.IndexName)
	}
	fmt.Fprintf(b, " (rows=%.0f time=%s)", node.ActualRows, node.ActualTotalTime)
	b.WriteString("\n")
	for _, child := range node.Children {
		renderPlanNode(b, child, depth+1)
	}
}

func percent(before, after time.Duration) string {
	if before == 0 {
		return "N/A"
	}
	delta := float64(after-before) / float64(before) * 100
	return fmt.Sprintf("%+.1f%%", delta)
}

func percentFloat(before, after float64) string {
	if before == 0 {
		return "N/A"
	}
	delta := (after - before) / before * 100
	return fmt.Sprintf("%+.1f%%", delta)
}

func Generate(data ReportData, outPath string) error {
	funcMap := template.FuncMap{
		"percent":      percent,
		"percentFloat": percentFloat,
		"renderPlan":   renderPlan,
	}

	tmpl, err := template.New("report").Funcs(funcMap).Parse(reportHTML)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	td := templateData{
		ReportData:     data,
		ChartJS:        template.JS(chartJS),
		LatencyMetrics: latencyMetrics,
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create report file: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, td); err != nil {
		return fmt.Errorf("render report: %w", err)
	}

	return nil
}
