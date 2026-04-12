package pgcompare

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const explainQuery = "EXPLAIN (ANALYZE, FORMAT JSON)"

type explainResult []struct {
	Plan explainNode `json:"Plan"`
}

type explainNode struct {
	NodeType        string        `json:"Node Type"`
	RelationName    string        `json:"Relation Name"`
	IndexName       string        `json:"Index Name"`
	ActualRows      float64       `json:"Actual Rows"`
	ActualTotalTime float64       `json:"Actual Total Time"`
	Plans           []explainNode `json:"Plans"`
}

type benchmark struct {
	log *slog.Logger
	db  *sql.DB
}

func NewBenchmark(log *slog.Logger, dsn string) (*benchmark, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to provide connection: %w", err)
	}

	return &benchmark{
		log: log,
		db:  db,
	}, nil
}

func (b *benchmark) ParseQueries(path string) ([]Query, error) {
	b.log.Info("Parsing queries", "path", path)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")

	var (
		queries     []Query
		currentName string
		currentSQL  []string
		seen        = make(map[string]struct{})
	)

	flush := func() error {
		if currentName == "" {
			return nil
		}
		sql := strings.TrimSpace(strings.Join(currentSQL, "\n"))
		if sql == "" {
			return fmt.Errorf("query %q has empty SQL", currentName)
		}
		if _, ok := seen[currentName]; ok {
			return fmt.Errorf("duplicate query name %q", currentName)
		}
		seen[currentName] = struct{}{}
		queries = append(queries, Query{
			Name: currentName,
			SQL:  sql,
		})
		currentName = ""
		currentSQL = nil
		return nil
	}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "-- name:") {
			if err := flush(); err != nil {
				return nil, fmt.Errorf("failed to flush query %d: %w", i, err)
			}

			name := strings.TrimSpace(strings.TrimPrefix(trimmed, "-- name:"))
			if name == "" {
				return nil, fmt.Errorf("empty query name at line %d", i)
			}

			currentName = name
			currentSQL = nil
			continue
		}
		if currentName != "" {
			currentSQL = append(currentSQL, line)
		}
	}

	if err := flush(); err != nil {
		return nil, fmt.Errorf("failed to flush queries: %w", err)
	}
	if len(queries) == 0 {
		return nil, fmt.Errorf("no named queries found in %s", path)
	}

	return queries, nil
}

func (b *benchmark) ValidateMatchingQueryNames(beforeQueries, afterQueries []Query) error {
	b.log.Info("Validating queries", "before", beforeQueries, "after", afterQueries)

	before := make(map[string]struct{}, len(beforeQueries))
	after := make(map[string]struct{}, len(afterQueries))

	for _, q := range beforeQueries {
		before[q.Name] = struct{}{}
	}
	for _, q := range afterQueries {
		after[q.Name] = struct{}{}
	}

	var missingInAfter []string
	for name := range before {
		if _, ok := after[name]; !ok {
			missingInAfter = append(missingInAfter, name)
		}
	}

	var missingInBefore []string
	for name := range after {
		if _, ok := before[name]; !ok {
			missingInBefore = append(missingInBefore, name)
		}
	}

	if len(missingInAfter) > 0 || len(missingInBefore) > 0 {
		return fmt.Errorf(
			"query names mismatch: missing in after=%v, missing in before=%v",
			missingInAfter,
			missingInBefore,
		)
	}

	return nil
}

func (b *benchmark) Run(ctx context.Context, queries []Query, iterations, concurrency uint) ([]Stats, error) {
	b.log.Info("Running queries", "queries", queries, "iterations", iterations)

	if iterations == 0 {
		return nil, fmt.Errorf("iterations cannot be zero")
	}
	if concurrency == 0 {
		return nil, fmt.Errorf("concurrency cannot be zero")
	}

	stats := make([]Stats, len(queries))
	for i, q := range queries {
		stat, err := b.runQueryBenchmark(ctx, q, iterations, concurrency)
		if err != nil {
			return nil, fmt.Errorf("benchmark query %q: %w", q.Name, err)
		}
		stats[i] = stat
	}

	return stats, nil
}

func (b *benchmark) runQueryBenchmark(ctx context.Context, q Query, iterations, concurrency uint) (Stats, error) {
	b.log.Info("Running query", "query", q.Name, "iterations", iterations)

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		durations []time.Duration
		errors    []string
	)

	errFlush := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		errors = append(errors, err.Error())
	}

	startWall := time.Now()
	base := iterations / concurrency
	extra := iterations % concurrency

	wg.Add(int(concurrency))

	for worker := range concurrency {
		runs := base
		if worker < extra {
			runs++
		}
		go func() {
			defer wg.Done()
			for range runs {
				start := time.Now()
				rows, err := b.db.QueryContext(ctx, q.SQL)
				if err != nil {
					errFlush(err)
					continue
				}
				if err := drainRows(rows); err != nil {
					errFlush(err)
					continue
				}
				elapsed := time.Since(start)
				func() {
					mu.Lock()
					defer mu.Unlock()
					durations = append(durations, elapsed)
				}()
			}
		}()
	}

	wg.Wait()
	totalWall := time.Since(startWall)

	return buildStats(q.Name, durations, errors, iterations, totalWall), nil
}

func (b *benchmark) Explain(ctx context.Context, queries []Query) ([]*PlanNode, error) {
	b.log.Info("Explaining queries", "queries", queries)

	if len(queries) == 0 {
		return nil, fmt.Errorf("no queries found")
	}
	plans := make([]*PlanNode, len(queries))
	for i, q := range queries {
		b.log.Info("Running explain query", "query", q.Name)

		query := explainQuery + " " + q.SQL
		var raw []byte
		err := b.db.QueryRowContext(ctx, query).Scan(&raw)
		if err != nil {
			return nil, fmt.Errorf("failed to explain query %q: %w", q.Name, err)
		}
		var result explainResult
		if err := json.Unmarshal(raw, &result); err != nil {
			return nil, fmt.Errorf("failed to unmarshal explain result: %w", err)
		}
		if len(result) == 0 {
			return nil, fmt.Errorf("empty explain result for query %q", q.Name)
		}
		plan := convertPlanNode(result[0].Plan)
		plans[i] = plan
	}

	return plans, nil
}

func (b *benchmark) DiffPlans(
	beforeQueries []Query,
	beforePlans []*PlanNode,
	afterQueries []Query,
	afterPlans []*PlanNode,
) ([]PlanDiff, error) {
	b.log.Info("Running diff plans", "queries", beforeQueries, "queries", beforePlans)

	if len(beforeQueries) != len(beforePlans) {
		return nil, fmt.Errorf("before queries/plans mismatch: %d queries, %d plans", len(beforeQueries), len(beforePlans))
	}
	if len(afterQueries) != len(afterPlans) {
		return nil, fmt.Errorf("after queries/plans mismatch: %d queries, %d plans", len(afterQueries), len(afterPlans))
	}

	beforeByName := make(map[string]*PlanNode, len(beforeQueries))
	for i, q := range beforeQueries {
		beforeByName[q.Name] = beforePlans[i]
	}

	afterByName := make(map[string]*PlanNode, len(afterQueries))
	for i, q := range afterQueries {
		afterByName[q.Name] = afterPlans[i]
	}

	diffs := make([]PlanDiff, 0, len(beforeQueries))

	for _, q := range beforeQueries {
		beforePlan, ok := beforeByName[q.Name]
		if !ok {
			return nil, fmt.Errorf("missing before plan for query %q", q.Name)
		}

		afterPlan, ok := afterByName[q.Name]
		if !ok {
			return nil, fmt.Errorf("missing after plan for query %q", q.Name)
		}

		summary := summarizePlanDiff(beforePlan, afterPlan)
		if len(summary) == 0 {
			summary = []string{"No significant plan changes detected"}
		}

		diffs = append(diffs, PlanDiff{
			QueryName: q.Name,
			Before:    beforePlan,
			After:     afterPlan,
			Summary:   summary,
		})
	}

	return diffs, nil
}

func convertPlanNode(node explainNode) *PlanNode {
	children := make([]*PlanNode, 0, len(node.Plans))
	for _, child := range node.Plans {
		children = append(children, convertPlanNode(child))
	}

	return &PlanNode{
		NodeType:        node.NodeType,
		RelationName:    node.RelationName,
		IndexName:       node.IndexName,
		ActualRows:      node.ActualRows,
		ActualTotalTime: time.Duration(node.ActualTotalTime * float64(time.Millisecond)),
		Children:        children,
	}
}

func (b *benchmark) ReadinessCheck(ctx context.Context, queries []Query) error {
	if err := b.db.PingContext(ctx); err != nil {
		return fmt.Errorf("postgres unreachable: %w", err)
	}

	var n int
	err := b.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
	`).Scan(&n)
	if err != nil {
		return fmt.Errorf("check schema: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("no tables in public schema — migrations may have failed")
	}

	for _, q := range queries {
		rows, err := b.db.QueryContext(ctx, q.SQL)
		if err != nil {
			return fmt.Errorf("query %q failed dry-run: %w", q.Name, err)
		}
		if err := drainRows(rows); err != nil {
			return fmt.Errorf("query %q dry-run: %w", q.Name, err)
		}
	}

	return nil
}

func (b *benchmark) Close() error {
	return b.db.Close()
}

func drainRows(rows *sql.Rows) error {
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("failed to get columns: %w", err)
	}
	values := make([]any, len(cols))
	dest := make([]any, len(cols))
	for i := range values {
		dest[i] = &values[i]
	}
	for rows.Next() {
		if err := rows.Scan(dest...); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}
	}

	return rows.Err()
}

func buildStats(name string, durations []time.Duration, errors []string, iterations uint, totalWall time.Duration) Stats {
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })

	stat := Stats{
		QueryName: name,
		ErrorRate: float64(len(errors)) / float64(iterations),
		Errors:    errors,
	}

	if len(durations) == 0 {
		return stat
	}

	stat.Min = durations[0]
	stat.Max = durations[len(durations)-1]
	stat.P50 = percentile(durations, 0.50)
	stat.P95 = percentile(durations, 0.95)
	stat.P99 = percentile(durations, 0.99)

	var sum time.Duration
	for _, d := range durations {
		sum += d
	}
	stat.Mean = sum / time.Duration(len(durations))

	meanF := float64(stat.Mean)
	var sumSq float64
	for _, d := range durations {
		diff := float64(d) - meanF
		sumSq += diff * diff
	}
	stat.StdDev = time.Duration(math.Sqrt(sumSq / float64(len(durations))))

	if totalWall > 0 {
		stat.QPS = float64(len(durations)) / totalWall.Seconds()
	}

	return stat
}

func percentile(values []time.Duration, p float64) time.Duration {
	if len(values) == 0 {
		return 0
	}

	idx := int(math.Ceil(float64(len(values))*p)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(values) {
		idx = len(values) - 1
	}

	return values[idx]
}

func summarizePlanDiff(before, after *PlanNode) []string {
	var summary []string
	seen := make(map[string]struct{})

	var walk func(before, after *PlanNode)
	walk = func(before, after *PlanNode) {
		if before == nil || after == nil {
			return
		}

		add := func(msg string) {
			if msg == "" {
				return
			}
			if _, ok := seen[msg]; ok {
				return
			}
			seen[msg] = struct{}{}
			summary = append(summary, msg)
		}

		if before.NodeType != after.NodeType {
			msg := fmt.Sprintf("%s -> %s", before.NodeType, after.NodeType)
			if before.RelationName != "" || after.RelationName != "" {
				relation := after.RelationName
				if relation == "" {
					relation = before.RelationName
				}
				msg += fmt.Sprintf(" on %s", relation)
			}
			add(msg)
		}

		if before.NodeType == "Sort" && after.NodeType != "Sort" {
			add("Explicit Sort removed")
		}
		if before.NodeType != "Sort" && after.NodeType == "Sort" {
			add("Explicit Sort added")
		}

		if before.IndexName != after.IndexName && (before.IndexName != "" || after.IndexName != "") {
			switch {
			case before.IndexName == "":
				add(fmt.Sprintf("Index added: %s", after.IndexName))
			case after.IndexName == "":
				add(fmt.Sprintf("Index removed: %s", before.IndexName))
			default:
				add(fmt.Sprintf("Index changed: %s -> %s", before.IndexName, after.IndexName))
			}
		}

		if before.NodeType == "Seq Scan" && strings.Contains(after.NodeType, "Index") {
			target := after.IndexName
			if target == "" {
				target = after.RelationName
			}
			add(fmt.Sprintf("Seq Scan replaced with %s on %s", after.NodeType, target))
		}

		if before.ActualRows != after.ActualRows {
			add(fmt.Sprintf("Actual rows changed: %.0f -> %.0f", before.ActualRows, after.ActualRows))
		}

		n := len(before.Children)
		if len(after.Children) < n {
			n = len(after.Children)
		}
		for i := 0; i < n; i++ {
			if len(summary) >= 5 {
				return
			}
			walk(before.Children[i], after.Children[i])
		}

		if len(summary) >= 5 {
			return
		}
		if len(before.Children) > len(after.Children) {
			add(fmt.Sprintf("Removed %d child node(s)", len(before.Children)-len(after.Children)))
		}
		if len(after.Children) > len(before.Children) {
			add(fmt.Sprintf("Added %d child node(s)", len(after.Children)-len(before.Children)))
		}
	}

	walk(before, after)

	if len(summary) > 5 {
		summary = summary[:5]
	}

	return summary
}
