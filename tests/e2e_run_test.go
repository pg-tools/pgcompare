//go:build integration

package tests

import (
	"fmt"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunCommandE2E(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("integration test uses POSIX shell scripts")
	}

	repoRoot := repoRoot(t)
	composeCmd := requireDockerCompose(t)
	requireDockerDaemon(t, composeCmd)

	binaryPath := buildBinary(t, repoRoot)
	projectDir := t.TempDir()
	port := reservePort(t)
	reportPath := filepath.Join(projectDir, "report.html")
	composeProject := "pgcomparee2e" + port

	writeExecutable(t, filepath.Join(projectDir, "setup.sh"), setupScript())
	writeFile(t, filepath.Join(projectDir, ".env"), testEnvFile(port))
	writeFile(t, filepath.Join(projectDir, "pgcompare.yaml"), testConfigYAML())
	writeFile(t, filepath.Join(projectDir, "docker-compose.yml"), dockerComposeYAML())
	writeFile(t, filepath.Join(projectDir, "queries_before.sql"), testQueriesSQL())
	writeFile(t, filepath.Join(projectDir, "queries_after.sql"), testQueriesSQL())
	writeFile(t, filepath.Join(projectDir, "schema.sql"), testSchemaSQL())

	cmdEnv := append(os.Environ(), "COMPOSE_PROJECT_NAME="+composeProject)
	t.Cleanup(func() {
		cleanupDockerProject(projectDir, cmdEnv, composeCmd)
	})

	cmd := exec.Command(
		binaryPath,
		"run",
		"--config", filepath.Join(projectDir, "pgcompare.yaml"),
		"--out", reportPath,
		"--verbose",
	)
	cmd.Dir = projectDir
	cmd.Env = cmdEnv

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	report, err := os.ReadFile(reportPath)
	require.NoError(t, err)

	html := string(report)
	before := extractReportStat(t, html, "before", "find_active_users")
	after := extractReportStat(t, html, "after", "find_active_users")
	speedups := extractSpeedups(t, html)
	diff := extractPlanDiff(t, html, "find_active_users")

	assert.Contains(t, html, "find_active_users")
	assert.Contains(t, html, "idx_users_active_created_at")
	assert.Contains(t, html, "QueryName:\"find_active_users\"")
	assert.Contains(t, html, "WarmupIterations: 1")
	assert.Contains(t, html, "warmup iterations:")
	assert.Contains(t, html, "Add an index for active users ordered by creation date.")
	assert.Contains(t, html, "The ordered lookup should use the new index.")

	assert.Greater(t, before.P95, int64(0))
	assert.Greater(t, after.P95, int64(0))
	assert.Greater(t, before.QPS, 0.0)
	assert.Greater(t, after.QPS, 0.0)
	assert.Len(t, speedups, 1)
	assert.Greater(t, speedups[0], 0.0)

	metricsDiffer := before.P95 != after.P95 || math.Abs(before.QPS-after.QPS) > 0.0001
	assert.True(t, metricsDiffer, "expected before and after benchmark metrics to differ")

	assert.NotEmpty(t, diff.Summary)
	assert.NotContains(t, diff.Summary, "No significant plan changes detected")
	assert.Contains(t, diff.BeforeText, "Seq Scan")
	assert.Contains(t, diff.AfterText, "Index")
}

func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)

	return filepath.Clean(filepath.Join(filepath.Dir(file), ".."))
}

func requireDockerCompose(t *testing.T) []string {
	t.Helper()

	candidates := [][]string{
		{"docker", "compose"},
		{"docker-compose"},
	}

	for _, candidate := range candidates {
		cmd := exec.Command(candidate[0], append(candidate[1:], "version")...)
		if err := cmd.Run(); err == nil {
			return candidate
		}
	}

	t.Skip("docker compose is not available in PATH")
	return nil
}

func requireDockerDaemon(t *testing.T, composeCmd []string) {
	t.Helper()

	dockerBin, err := exec.LookPath("docker")
	if err != nil {
		t.Skip("docker CLI is not available")
	}

	cmd := exec.Command(dockerBin, "info")
	if err := cmd.Run(); err != nil {
		t.Skip("docker daemon is not available")
	}
}

func cleanupDockerProject(projectDir string, env []string, composeCmd []string) {
	args := append(append([]string{}, composeCmd...), "down", "-v")
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = projectDir
	cmd.Env = env
	_ = cmd.Run()
}

func buildBinary(t *testing.T, repoRoot string) string {
	t.Helper()

	binaryPath := filepath.Join(t.TempDir(), "pgcompare")

	cmd := exec.Command("go", "build", "-o", binaryPath, ".")
	cmd.Dir = repoRoot

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	return binaryPath
}

type reportStat struct {
	Min       int64
	Max       int64
	P50       int64
	P95       int64
	P99       int64
	Mean      int64
	StdDev    int64
	QPS       float64
	ErrorRate float64
}

type planDiff struct {
	Summary    string
	BeforeText string
	AfterText  string
}

func extractReportStat(t *testing.T, html, phase, query string) reportStat {
	t.Helper()

	section := extractSection(t, html, "var "+phase+"Stats = [];", sectionEndMarker(phase))
	pattern := fmt.Sprintf(
		`(?s)QueryName:\s*"%s".*?Min:\s*(\d+)\s*,.*?Max:\s*(\d+)\s*,.*?P50:\s*(\d+)\s*,.*?P95:\s*(\d+)\s*,.*?P99:\s*(\d+)\s*,.*?Mean:\s*(\d+)\s*,.*?StdDev:\s*(\d+)\s*,.*?QPS:\s*([0-9.eE+\-]+)\s*,.*?ErrorRate:\s*([0-9.eE+\-]+)`,
		regexp.QuoteMeta(query),
	)

	matches := regexp.MustCompile(pattern).FindStringSubmatch(section)
	require.Len(t, matches, 10, "failed to extract %s stats for %s from %q", phase, query, preview(section, 300))

	return reportStat{
		Min:       parseInt64(t, matches[1]),
		Max:       parseInt64(t, matches[2]),
		P50:       parseInt64(t, matches[3]),
		P95:       parseInt64(t, matches[4]),
		P99:       parseInt64(t, matches[5]),
		Mean:      parseInt64(t, matches[6]),
		StdDev:    parseInt64(t, matches[7]),
		QPS:       parseFloat64(t, matches[8]),
		ErrorRate: parseFloat64(t, matches[9]),
	}
}

func extractSpeedups(t *testing.T, html string) []float64 {
	t.Helper()

	matches := regexp.MustCompile(`var speedups = \[([^]]*)];`).FindStringSubmatch(html)
	require.Len(t, matches, 2, "failed to extract speedups from report")

	parts := strings.Split(matches[1], ",")
	values := make([]float64, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		values = append(values, parseFloat64(t, part))
	}

	return values
}

func extractPlanDiff(t *testing.T, html, query string) planDiff {
	t.Helper()

	section := extractSection(t, html, "var diffs = [];", "var speedups = [")
	pattern := fmt.Sprintf(
		`(?s)diffs\.push\(\{QueryName:\s*"%s"\s*,Summary:\s*\[(.*?)\]\s*,BeforeText:\s*"(.*?)"\s*,AfterText:\s*"(.*?)"\}\);`,
		regexp.QuoteMeta(query),
	)

	matches := regexp.MustCompile(pattern).FindStringSubmatch(section)
	require.Len(t, matches, 4, "failed to extract plan diff for %s from %q", query, preview(section, 300))

	return planDiff{
		Summary:    matches[1],
		BeforeText: matches[2],
		AfterText:  matches[3],
	}
}

func parseInt64(t *testing.T, value string) int64 {
	t.Helper()

	result, err := strconv.ParseInt(value, 10, 64)
	require.NoError(t, err)

	return result
}

func parseFloat64(t *testing.T, value string) float64 {
	t.Helper()

	result, err := strconv.ParseFloat(value, 64)
	require.NoError(t, err)

	return result
}

func extractSection(t *testing.T, html, startMarker, endMarker string) string {
	t.Helper()

	start := strings.Index(html, startMarker)
	require.NotEqual(t, -1, start, "failed to find report section start %q", startMarker)

	rest := html[start:]
	end := strings.Index(rest, endMarker)
	require.NotEqual(t, -1, end, "failed to find report section end %q", endMarker)

	return rest[:end]
}

func sectionEndMarker(phase string) string {
	switch phase {
	case "before":
		return "var afterStats = [];"
	case "after":
		return "var diffs = [];"
	default:
		return "return {"
	}
}

func preview(text string, limit int) string {
	text = strings.ReplaceAll(text, "\n", `\n`)
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}

func reservePort(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	addr, ok := listener.Addr().(*net.TCPAddr)
	require.True(t, ok)

	return fmt.Sprintf("%d", addr.Port)
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o755))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func setupScript() string {
	return strings.TrimSpace(`
#!/bin/sh
set -eu

$DOCKER_COMPOSE up -d postgres

attempts=60
until $DOCKER_COMPOSE exec -T postgres pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB" >/dev/null 2>&1
do
  attempts=$((attempts - 1))
  if [ "$attempts" -le 0 ]; then
    echo "postgres container did not become ready" >&2
    exit 1
  fi
  sleep 1
done

$DOCKER_COMPOSE exec -T postgres psql \
  -U "$POSTGRES_USER" \
  -d "$POSTGRES_DB" \
  -v ON_ERROR_STOP=1 < ./schema.sql

if [ "$MIGRATION_VERSION" = "2" ]; then
  $DOCKER_COMPOSE exec -T postgres psql \
    -U "$POSTGRES_USER" \
    -d "$POSTGRES_DB" \
    -v ON_ERROR_STOP=1 \
    -c "CREATE INDEX idx_users_active_created_at ON users (active, created_at DESC);"
fi
` + "\n")
}

func testEnvFile(port string) string {
	return fmt.Sprintf(strings.TrimSpace(`
POSTGRES_USER=postgres
POSTGRES_PASSWORD=postgres
POSTGRES_DB=app
POSTGRES_PORT=%s
MIGRATION_VERSION=0
`)+"\n", port)
}

func testConfigYAML() string {
	return strings.TrimSpace(`
migration:
  env_var: MIGRATION_VERSION
  before_version: "1"
  after_version: "2"

setup:
  command: "./setup.sh"

benchmark:
  before_queries: queries_before.sql
  after_queries: queries_after.sql
  warmup_iterations: 1
  iterations: 3
  concurrency: 1

report:
  description:
    - query: "find_active_users"
      what: "Add an index for active users ordered by creation date."
      changes: |
        CREATE INDEX idx_users_active_created_at
          ON users (active, created_at DESC);
      expected: "The ordered lookup should use the new index."
` + "\n")
}

func dockerComposeYAML() string {
	return strings.TrimSpace(`
services:
  postgres:
    image: postgres:17-alpine
    ports:
      - "127.0.0.1:${POSTGRES_PORT}:5432"
    environment:
      POSTGRES_USER: ${POSTGRES_USER}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      POSTGRES_DB: ${POSTGRES_DB}
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U $$POSTGRES_USER -d $$POSTGRES_DB"]
      interval: 1s
      timeout: 5s
      retries: 30
` + "\n")
}

func testQueriesSQL() string {
	return strings.TrimSpace(`
-- name: find_active_users
SELECT id, email, created_at
FROM users
WHERE active = true
ORDER BY created_at DESC
LIMIT 5;
` + "\n")
}

func testSchemaSQL() string {
	return strings.TrimSpace(`
CREATE TABLE users (
  id BIGSERIAL PRIMARY KEY,
  email TEXT NOT NULL,
  active BOOLEAN NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
);

INSERT INTO users (email, active, created_at)
SELECT
  'user-' || gs || '@example.com',
  gs % 100 = 0,
  NOW() - make_interval(mins => gs)
FROM generate_series(1, 100000) AS gs;
` + "\n")
}
