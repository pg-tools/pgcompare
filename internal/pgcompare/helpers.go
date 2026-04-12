package pgcompare

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"os/exec"
	"runtime"
	"sort"
	"strings"
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

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd", "/C", command)
	}
	return exec.CommandContext(ctx, "sh", "-c", command)
}

func envWithOverride(env []string, key, value string) []string {
	out := make([]string, len(env))
	copy(out, env)

	for i, item := range out {
		name, _, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		if sameEnvKey(name, key) {
			out[i] = key + "=" + value
			return out
		}
	}

	return append(out, key+"="+value)
}

func sameEnvKey(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func detectDockerCompose() ([]string, error) {
	if err := exec.Command("docker", "compose", "version").Run(); err == nil {
		return []string{"docker", "compose"}, nil
	}

	if err := exec.Command("docker-compose", "version").Run(); err == nil {
		return []string{"docker-compose"}, nil
	}

	return nil, fmt.Errorf("docker compose v2 or docker-compose v1 is required")
}
