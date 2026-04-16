package pgcompare

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestParseQueries(t *testing.T) {
	bench := &benchmark{log: newTestLogger()}

	tests := []struct {
		name        string
		fileContent string
		missingFile bool
		wantQueries []Query
		wantErr     string
	}{
		{
			name:        "two valid queries",
			fileContent: "-- name: q1\nSELECT 1;\n-- name: q2\nSELECT 2;",
			wantQueries: []Query{{Name: "q1", SQL: "SELECT 1;"}, {Name: "q2", SQL: "SELECT 2;"}},
		},
		{
			name:        "duplicate query name",
			fileContent: "-- name: q1\nSELECT 1;\n-- name: q1\nSELECT 2;",
			wantErr:     "duplicate query name",
		},
		{
			name:        "empty sql in query",
			fileContent: "-- name: q1\n-- name: q2\nSELECT 1;",
			wantErr:     "empty SQL",
		},
		{
			name:        "no name markers",
			fileContent: "SELECT 1;",
			wantErr:     "no named queries",
		},
		{
			name:        "empty query name",
			fileContent: "-- name: \nSELECT 1;",
			wantErr:     "empty query name",
		},
		{
			name:        "missing file",
			missingFile: true,
			wantErr:     "failed to open file",
		},
		{
			name:        "single multiline sql",
			fileContent: "-- name: q1\nSELECT\n  id\nFROM t;",
			wantQueries: []Query{{Name: "q1", SQL: "SELECT\n  id\nFROM t;"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			path := filepath.Join(tempDir, "queries.sql")
			if !tt.missingFile {
				require.NoError(t, os.WriteFile(path, []byte(tt.fileContent), 0o644))
			}

			queries, err := bench.ParseQueries(path)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantQueries, queries)
		})
	}
}

func TestValidateMatchingQueryNames(t *testing.T) {
	bench := &benchmark{log: newTestLogger()}

	tests := []struct {
		name    string
		before  []Query
		after   []Query
		wantErr string
	}{
		{
			name:   "same names",
			before: queryNames("q1", "q2"),
			after:  queryNames("q1", "q2"),
		},
		{
			name:    "extra in before",
			before:  queryNames("q1", "q2"),
			after:   queryNames("q1"),
			wantErr: "missing in after",
		},
		{
			name:    "extra in after",
			before:  queryNames("q1"),
			after:   queryNames("q1", "q2"),
			wantErr: "missing in before",
		},
		{
			name:   "both empty",
			before: nil,
			after:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := bench.ValidateMatchingQueryNames(tt.before, tt.after)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestBuildStats(t *testing.T) {
	tests := []struct {
		name       string
		durations  []time.Duration
		errors     []string
		iterations uint
		totalWall  time.Duration
		assertion  func(t *testing.T, got Stats)
	}{
		{
			name: "normal sample",
			durations: []time.Duration{
				1 * time.Millisecond,
				2 * time.Millisecond,
				3 * time.Millisecond,
				4 * time.Millisecond,
				5 * time.Millisecond,
				6 * time.Millisecond,
				7 * time.Millisecond,
				8 * time.Millisecond,
				9 * time.Millisecond,
				10 * time.Millisecond,
			},
			iterations: 10,
			totalWall:  1 * time.Second,
			assertion: func(t *testing.T, got Stats) {
				assert.Equal(t, 1*time.Millisecond, got.Min)
				assert.Equal(t, 10*time.Millisecond, got.Max)
				assert.Equal(t, 5*time.Millisecond, got.P50)
				assert.Equal(t, 10*time.Millisecond, got.P95)
				assert.Equal(t, 10*time.Millisecond, got.P99)
				assert.Equal(t, 5500*time.Microsecond, got.Mean)
				assert.Greater(t, got.StdDev, time.Duration(0))
				assert.Greater(t, got.QPS, 0.0)
				assert.Equal(t, 0.0, got.ErrorRate)
			},
		},
		{
			name:       "empty durations",
			durations:  nil,
			errors:     []string{"e1", "e2", "e3"},
			iterations: 3,
			totalWall:  1 * time.Second,
			assertion: func(t *testing.T, got Stats) {
				assert.Equal(t, 1.0, got.ErrorRate)
				assert.Equal(t, time.Duration(0), got.Min)
				assert.Equal(t, time.Duration(0), got.Max)
				assert.Equal(t, time.Duration(0), got.P50)
			},
		},
		{
			name:       "single duration",
			durations:  []time.Duration{7 * time.Millisecond},
			iterations: 1,
			totalWall:  500 * time.Millisecond,
			assertion: func(t *testing.T, got Stats) {
				assert.Equal(t, 7*time.Millisecond, got.Min)
				assert.Equal(t, 7*time.Millisecond, got.Max)
				assert.Equal(t, 7*time.Millisecond, got.P50)
				assert.Equal(t, 7*time.Millisecond, got.P95)
				assert.Equal(t, 7*time.Millisecond, got.P99)
				assert.Equal(t, time.Duration(0), got.StdDev)
			},
		},
		{
			name:       "zero wall time",
			durations:  []time.Duration{1 * time.Millisecond, 2 * time.Millisecond},
			iterations: 2,
			totalWall:  0,
			assertion: func(t *testing.T, got Stats) {
				assert.Equal(t, 0.0, got.QPS)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildStats("q", tt.durations, tt.errors, tt.iterations, tt.totalWall)
			tt.assertion(t, got)
		})
	}
}

func TestPercentile(t *testing.T) {
	tests := []struct {
		name   string
		values []time.Duration
		p      float64
		want   time.Duration
	}{
		{
			name:   "p50 of four",
			values: []time.Duration{1 * time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond, 4 * time.Millisecond},
			p:      0.50,
			want:   2 * time.Millisecond,
		},
		{
			name:   "p95 of twenty",
			values: durationsRange(1, 20),
			p:      0.95,
			want:   19 * time.Millisecond,
		},
		{
			name:   "p99 of hundred",
			values: durationsRange(1, 100),
			p:      0.99,
			want:   99 * time.Millisecond,
		},
		{
			name:   "p100",
			values: []time.Duration{1 * time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond},
			p:      1.0,
			want:   3 * time.Millisecond,
		},
		{
			name:   "p0",
			values: []time.Duration{1 * time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond},
			p:      0.0,
			want:   1 * time.Millisecond,
		},
		{
			name:   "single element",
			values: []time.Duration{5 * time.Millisecond},
			p:      0.95,
			want:   5 * time.Millisecond,
		},
		{
			name:   "empty slice",
			values: nil,
			p:      0.50,
			want:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, percentile(tt.values, tt.p))
		})
	}
}

func TestWarmupValidation(t *testing.T) {
	bench := &benchmark{log: newTestLogger()}

	require.NoError(t, bench.Warmup(context.Background(), Query{Name: "q1", SQL: "SELECT 1"}, 0, 1))

	err := bench.Warmup(context.Background(), Query{Name: "q1", SQL: "SELECT 1"}, 1, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "warmup concurrency cannot be zero")
}

func TestSummarizePlanDiff(t *testing.T) {
	t.Run("identical plans through DiffPlans", func(t *testing.T) {
		bench := &benchmark{log: newTestLogger()}
		queries := []Query{{Name: "q1", SQL: "SELECT 1"}}
		plan := &PlanNode{NodeType: "Seq Scan", RelationName: "t"}

		diffs, err := bench.DiffPlans(queries, []*PlanNode{plan}, queries, []*PlanNode{plan})
		require.NoError(t, err)
		require.Len(t, diffs, 1)
		assert.Equal(t, []string{"No significant plan changes detected"}, diffs[0].Summary)
	})

	t.Run("seq scan replaced with index scan", func(t *testing.T) {
		before := &PlanNode{NodeType: "Seq Scan", RelationName: "users"}
		after := &PlanNode{NodeType: "Index Scan", RelationName: "users", IndexName: "idx_users"}

		summary := summarizePlanDiff(before, after)
		assert.Contains(t, summary, "Seq Scan replaced with Index Scan on idx_users")
	})

	t.Run("index changed", func(t *testing.T) {
		before := &PlanNode{NodeType: "Index Scan", RelationName: "users", IndexName: "idx_a"}
		after := &PlanNode{NodeType: "Index Scan", RelationName: "users", IndexName: "idx_b"}

		summary := summarizePlanDiff(before, after)
		assert.Contains(t, summary, "Index changed: idx_a -> idx_b")
	})

	t.Run("sort added", func(t *testing.T) {
		before := &PlanNode{NodeType: "Seq Scan", RelationName: "users"}
		after := &PlanNode{NodeType: "Sort", RelationName: "users"}

		summary := summarizePlanDiff(before, after)
		assert.Contains(t, summary, "Explicit Sort added")
	})

	t.Run("sort removed", func(t *testing.T) {
		before := &PlanNode{NodeType: "Sort", RelationName: "users"}
		after := &PlanNode{NodeType: "Seq Scan", RelationName: "users"}

		summary := summarizePlanDiff(before, after)
		assert.Contains(t, summary, "Explicit Sort removed")
	})

	t.Run("child nodes removed", func(t *testing.T) {
		before := &PlanNode{
			NodeType: "Hash Join",
			Children: []*PlanNode{
				{NodeType: "Seq Scan", RelationName: "a"},
				{NodeType: "Seq Scan", RelationName: "b"},
			},
		}
		after := &PlanNode{
			NodeType: "Hash Join",
			Children: []*PlanNode{
				{NodeType: "Seq Scan", RelationName: "a"},
			},
		}

		summary := summarizePlanDiff(before, after)
		found := false
		for _, s := range summary {
			if s == "Removed 1 child node(s)" {
				found = true
			}
		}
		assert.True(t, found, "expected 'Removed 1 child node(s)' in %v", summary)
	})

	t.Run("child nodes added", func(t *testing.T) {
		before := &PlanNode{
			NodeType: "Hash Join",
			Children: []*PlanNode{
				{NodeType: "Seq Scan", RelationName: "a"},
			},
		}
		after := &PlanNode{
			NodeType: "Hash Join",
			Children: []*PlanNode{
				{NodeType: "Seq Scan", RelationName: "a"},
				{NodeType: "Seq Scan", RelationName: "b"},
			},
		}

		summary := summarizePlanDiff(before, after)
		found := false
		for _, s := range summary {
			if s == "Added 1 child node(s)" {
				found = true
			}
		}
		assert.True(t, found, "expected 'Added 1 child node(s)' in %v", summary)
	})

	t.Run("actual rows changed", func(t *testing.T) {
		before := &PlanNode{NodeType: "Seq Scan", RelationName: "t", ActualRows: 100}
		after := &PlanNode{NodeType: "Seq Scan", RelationName: "t", ActualRows: 50}

		summary := summarizePlanDiff(before, after)
		assert.Contains(t, summary, "Actual rows changed: 100 -> 50")
	})

	t.Run("index added", func(t *testing.T) {
		before := &PlanNode{NodeType: "Seq Scan", RelationName: "users"}
		after := &PlanNode{NodeType: "Seq Scan", RelationName: "users", IndexName: "idx_new"}

		summary := summarizePlanDiff(before, after)
		assert.Contains(t, summary, "Index added: idx_new")
	})

	t.Run("index removed", func(t *testing.T) {
		before := &PlanNode{NodeType: "Seq Scan", RelationName: "users", IndexName: "idx_old"}
		after := &PlanNode{NodeType: "Seq Scan", RelationName: "users"}

		summary := summarizePlanDiff(before, after)
		assert.Contains(t, summary, "Index removed: idx_old")
	})

	t.Run("summary length is capped at five", func(t *testing.T) {
		before := &PlanNode{
			NodeType:     "Seq Scan",
			RelationName: "t",
			ActualRows:   10,
			Children: []*PlanNode{
				{NodeType: "Sort", ActualRows: 10},
				{NodeType: "Seq Scan", RelationName: "a", ActualRows: 1},
				{NodeType: "Seq Scan", RelationName: "b", ActualRows: 1},
			},
		}
		after := &PlanNode{
			NodeType:     "Index Scan",
			RelationName: "t",
			IndexName:    "idx_t",
			ActualRows:   20,
			Children: []*PlanNode{
				{NodeType: "Hash", ActualRows: 20},
				{NodeType: "Index Scan", RelationName: "a", IndexName: "idx_a", ActualRows: 2},
				{NodeType: "Index Scan", RelationName: "b", IndexName: "idx_b", ActualRows: 2},
				{NodeType: "Sort", ActualRows: 3},
			},
		}

		summary := summarizePlanDiff(before, after)
		assert.NotEmpty(t, summary)
		assert.LessOrEqual(t, len(summary), 5)
	})
}

func TestConvertPlanNode(t *testing.T) {
	t.Run("single node", func(t *testing.T) {
		node := explainNode{
			NodeType:        "Seq Scan",
			RelationName:    "users",
			IndexName:       "",
			ActualRows:      42,
			ActualTotalTime: 1.5,
		}

		result := convertPlanNode(node)

		assert.Equal(t, "Seq Scan", result.NodeType)
		assert.Equal(t, "users", result.RelationName)
		assert.Equal(t, "", result.IndexName)
		assert.Equal(t, 42.0, result.ActualRows)
		assert.Equal(t, time.Duration(1.5*float64(time.Millisecond)), result.ActualTotalTime)
		assert.Empty(t, result.Children)
	})

	t.Run("nested tree", func(t *testing.T) {
		node := explainNode{
			NodeType:        "Sort",
			ActualRows:      100,
			ActualTotalTime: 5.0,
			Plans: []explainNode{
				{
					NodeType:        "Index Scan",
					RelationName:    "orders",
					IndexName:       "idx_orders_date",
					ActualRows:      100,
					ActualTotalTime: 3.0,
					Plans: []explainNode{
						{
							NodeType:        "Seq Scan",
							RelationName:    "items",
							ActualRows:      200,
							ActualTotalTime: 1.0,
						},
					},
				},
			},
		}

		result := convertPlanNode(node)

		assert.Equal(t, "Sort", result.NodeType)
		require.Len(t, result.Children, 1)

		child := result.Children[0]
		assert.Equal(t, "Index Scan", child.NodeType)
		assert.Equal(t, "orders", child.RelationName)
		assert.Equal(t, "idx_orders_date", child.IndexName)
		require.Len(t, child.Children, 1)

		grandchild := child.Children[0]
		assert.Equal(t, "Seq Scan", grandchild.NodeType)
		assert.Equal(t, "items", grandchild.RelationName)
		assert.Empty(t, grandchild.Children)
	})
}

func TestDiffPlans(t *testing.T) {
	bench := &benchmark{log: newTestLogger()}

	t.Run("mismatched before queries and plans count", func(t *testing.T) {
		queries := []Query{{Name: "q1", SQL: "SELECT 1"}, {Name: "q2", SQL: "SELECT 2"}}
		plans := []*PlanNode{{NodeType: "Seq Scan"}}

		_, err := bench.DiffPlans(queries, plans, queries, []*PlanNode{{NodeType: "Seq Scan"}, {NodeType: "Seq Scan"}})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "before queries/plans mismatch")
	})

	t.Run("mismatched after queries and plans count", func(t *testing.T) {
		queries := []Query{{Name: "q1", SQL: "SELECT 1"}}
		plans := []*PlanNode{{NodeType: "Seq Scan"}}

		afterQueries := []Query{{Name: "q1", SQL: "SELECT 1"}, {Name: "q2", SQL: "SELECT 2"}}
		afterPlans := []*PlanNode{{NodeType: "Seq Scan"}}

		_, err := bench.DiffPlans(queries, plans, afterQueries, afterPlans)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "after queries/plans mismatch")
	})

	t.Run("missing after plan for query", func(t *testing.T) {
		beforeQueries := []Query{{Name: "q1", SQL: "SELECT 1"}}
		beforePlans := []*PlanNode{{NodeType: "Seq Scan"}}
		afterQueries := []Query{{Name: "q2", SQL: "SELECT 2"}}
		afterPlans := []*PlanNode{{NodeType: "Seq Scan"}}

		_, err := bench.DiffPlans(beforeQueries, beforePlans, afterQueries, afterPlans)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing after plan")
	})
}

func queryNames(names ...string) []Query {
	queries := make([]Query, 0, len(names))
	for _, name := range names {
		queries = append(queries, Query{Name: name})
	}
	return queries
}

func durationsRange(from, to int) []time.Duration {
	out := make([]time.Duration, 0, to-from+1)
	for i := from; i <= to; i++ {
		out = append(out, time.Duration(i)*time.Millisecond)
	}
	return out
}
