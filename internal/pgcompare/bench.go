package pgcompare

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

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
	fp, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute path: %w", err)
	}
	data, err := os.ReadFile(fp)
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

func (b *benchmark) Run(ctx context.Context, queries []Query, iterations, concurrency uint, phase string) (*BenchResult, error) {
	stats := make([]Stats, len(queries))
	for i, q := range queries {
		stat, err := b.runQueryBenchmark(ctx, q, iterations, concurrency)
		if err != nil {
			return nil, fmt.Errorf("benchmark query %q: %w", q.Name, err)
		}
		stats[i] = stat
	}

	return &BenchResult{
		Phase: phase,
		Stats: stats,
	}, nil
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

func (b *benchmark) Close() error {
	return b.db.Close()
}
