package pgcompare

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
