package pgcompare

import "time"

type Query struct {
	Name string
	SQL  string
}

type Stats struct {
	QueryName                     string
	Min, Max, P50, P95, P99, Mean time.Duration
	QPS                           float64
	ErrorRate                     float64
	Errors                        []string
}

type PlanNode struct {
	NodeType        string
	RelationName    string
	IndexName       string
	ActualRows      float64
	ActualTotalTime time.Duration
	Children        []*PlanNode
}

type PlanDiff struct {
	QueryName string
	Before    *PlanNode
	After     *PlanNode
	Summary   []string
}

type BenchResult struct {
	Phase string
	Stats []Stats
	Plans []*PlanNode
}

type ReportData struct {
	GeneratedAt time.Time
	Before      *BenchResult
	After       *BenchResult
	Diffs       []PlanDiff
	Description string
}
