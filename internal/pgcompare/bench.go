package pgcompare

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
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

		wg.Go(func() {
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
		})
	}

	wg.Wait()
	totalWall := time.Since(startWall)

	return buildStats(q.Name, durations, errors, iterations, totalWall), nil
}

func (b *benchmark) Explain(ctx context.Context, queries []Query) ([]*PlanNode, error) {
	if len(queries) == 0 {
		return nil, fmt.Errorf("no queries found")
	}
	plans := make([]*PlanNode, len(queries))
	for i, q := range queries {
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

func (b *benchmark) Close() error {
	return b.db.Close()
}
