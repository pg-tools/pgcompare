package pgcompare

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderPlan(t *testing.T) {
	t.Run("nil plan", func(t *testing.T) {
		assert.Equal(t, "(no plan)", renderPlan(nil))
	})

	t.Run("single node", func(t *testing.T) {
		plan := &PlanNode{
			NodeType:        "Seq Scan",
			RelationName:    "users",
			ActualRows:      100,
			ActualTotalTime: 5 * time.Millisecond,
		}

		rendered := renderPlan(plan)
		assert.Contains(t, rendered, "Seq Scan")
		assert.Contains(t, rendered, "users")
		assert.Contains(t, rendered, "rows=100")
	})

	t.Run("tree with children", func(t *testing.T) {
		plan := &PlanNode{
			NodeType:        "Sort",
			ActualRows:      100,
			ActualTotalTime: 6 * time.Millisecond,
			Children: []*PlanNode{
				{
					NodeType:        "Index Scan",
					RelationName:    "users",
					IndexName:       "idx_users",
					ActualRows:      100,
					ActualTotalTime: 4 * time.Millisecond,
				},
			},
		}

		rendered := renderPlan(plan)
		assert.Contains(t, rendered, "-> Sort")
		assert.Contains(t, rendered, "  -> Index Scan on users using idx_users")
	})
}

func TestFmtSpeedupMarksRegressions(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "report.html")

	err := Generate(ReportData{
		GeneratedAt: time.Now(),
		Speedups:    []float64{2.0, 0.5, 1.0},
		Before:      &BenchResult{Phase: "before"},
		After:       &BenchResult{Phase: "after"},
	}, out)
	require.NoError(t, err)

	html, err := os.ReadFile(out)
	require.NoError(t, err)

	body := string(html)

	assert.Contains(t, body, "f >= 1.05",
		"fmtSpeedup must treat values >= 1.05 as speedup")
	assert.Contains(t, body, "f <= 0.95",
		"fmtSpeedup must treat values <= 0.95 as regression")
	assert.Contains(t, body, "'badge-bad'",
		"fmtSpeedup must render regressions with badge-bad")
	assert.NotRegexp(t,
		`if \(!f \|\| f < 1\.05\) return \{ text: '1\.0\\u00d7', cls: 'badge-neutral' \};`,
		body,
		"old single-branch logic must be removed so regressions are no longer shown as neutral 1.0×")
}

func TestReportPlanTreesUseSharedScroller(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "report.html")

	err := Generate(ReportData{
		GeneratedAt: time.Now(),
		Before: &BenchResult{
			Phase: "before",
			Stats: []Stats{{QueryName: "q1"}},
		},
		After: &BenchResult{
			Phase: "after",
			Stats: []Stats{{QueryName: "q1"}},
		},
		Diffs: []PlanDiff{
			{
				QueryName: "q1",
				Before:    &PlanNode{NodeType: "Seq Scan"},
				After:     &PlanNode{NodeType: "Index Scan", IndexName: "idx_users"},
			},
		},
	}, out)
	require.NoError(t, err)

	html, err := os.ReadFile(out)
	require.NoError(t, err)

	body := string(html)

	assert.Contains(t, body, ".plan-scroll { margin-top: 14px; overflow-x: auto;",
		"plan trees should have one shared horizontal scrollbar")
	assert.Contains(t, body, ".plan-box { background: var(--code-bg); border-radius: 6px; padding: 14px; overflow: visible; }",
		"individual plan boxes must not create competing horizontal scrollbars")
	assert.Contains(t, body, "return '<div class=\"plan-scroll\"><div class=\"plan-trees\">' + before + after + '</div></div>';",
		"both plans should render inside the same scroll container")
	assert.Contains(t, body, "<div data-plan-container data-diff-index",
		"lazy container should be replaced by the shared scroller on open")
	assert.NotContains(t, body, "<div class=\"plan-trees\" data-plan-container",
		"the lazy container itself must not be the grid because renderPlanTrees now owns the grid wrapper")
}
