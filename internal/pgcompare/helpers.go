package pgcompare

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"time"
)

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
