package pgcompare

import (
	_ "embed"
	"fmt"
	"html/template"
	"os"
	"strings"
)

//go:embed templates/report.html
var reportHTML string

type templateData struct {
	ReportData
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

func Generate(data ReportData, outPath string) error {
	funcMap := template.FuncMap{
		"renderPlan": renderPlan,
	}

	tmpl, err := template.New("report").Funcs(funcMap).Parse(reportHTML)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create report file: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, templateData{ReportData: data}); err != nil {
		return fmt.Errorf("render report: %w", err)
	}

	return nil
}
