package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/spf13/cobra"

	"github.com/pg-tools/pgcompare/internal/pgcompare"
)

var Version = "dev"

var (
	flagConfig  string
	flagOut     string
	flagVerbose bool
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:     "pgcompare",
	Short:   "PostgreSQL query performance comparison tool",
	Version: Version,
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run benchmark and generate HTML report",
	RunE:  runBenchmark,
}

func init() {
	runCmd.Flags().StringVar(&flagConfig, "config", "", "path to pgcompare.yaml (required)")
	runCmd.Flags().StringVar(&flagOut, "out", "", "output path for report.html (default: next to config)")
	runCmd.Flags().BoolVarP(&flagVerbose, "verbose", "v", false, "verbose output")
	_ = runCmd.MarkFlagRequired("config")
	rootCmd.AddCommand(runCmd)
}

func runBenchmark(_ *cobra.Command, _ []string) error {
	logLevel := slog.LevelWarn
	if flagVerbose {
		logLevel = slog.LevelInfo
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))

	cfg, err := pgcompare.LoadConfig(flagConfig)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	outPath := flagOut
	if outPath == "" {
		outPath = filepath.Join(cfg.ProjectDir, "report.html")
	}

	// Parse queries
	bench, err := pgcompare.NewBenchmark(log, cfg.DSN)
	if err != nil {
		return fmt.Errorf("create benchmark: %w", err)
	}
	defer bench.Close()

	beforeQueries, err := bench.ParseQueries(filepath.Join(cfg.ProjectDir, cfg.Benchmark.BeforeQueries))
	if err != nil {
		return fmt.Errorf("parse before queries: %w", err)
	}
	afterQueries, err := bench.ParseQueries(filepath.Join(cfg.ProjectDir, cfg.Benchmark.AfterQueries))
	if err != nil {
		return fmt.Errorf("parse after queries: %w", err)
	}

	if err := bench.ValidateMatchingQueryNames(beforeQueries, afterQueries); err != nil {
		return err
	}

	// Setup docker
	docker, err := pgcompare.NewDockerComparator(log, cfg)
	if err != nil {
		return fmt.Errorf("create docker comparator: %w", err)
	}

	ctx := context.Background()
	defer func() {
		if err := docker.Cleanup(ctx); err != nil {
			log.Error("final cleanup failed", "err", err)
		}
	}()

	startAll := time.Now()
	printHeader("pgcompare")

	benchLabel := fmt.Sprintf("(%d repeat × %d iter × %d worker)",
		cfg.Benchmark.Repeats, cfg.Benchmark.Iterations, cfg.Benchmark.Concurrency)

	// Phase: before
	p := startPhase("Preparing 'before' environment")
	if err := docker.PrepareVersion(ctx, cfg.Migration.BeforeVersion); err != nil {
		p.Fail(err)
		return fmt.Errorf("prepare before: %w", err)
	}
	if err := bench.ReadinessCheck(ctx, beforeQueries); err != nil {
		p.Fail(err)
		return fmt.Errorf("before readiness: %w", err)
	}
	p.Done()

	p = startPhase("Benchmarking 'before' " + benchLabel)
	beforeStats, err := bench.RunRepeats(
		ctx,
		beforeQueries,
		uint(cfg.Benchmark.Repeats),
		uint(cfg.Benchmark.Iterations),
		uint(cfg.Benchmark.Concurrency),
		uint(cfg.Benchmark.WarmupIterations),
	)
	if err != nil {
		p.Fail(err)
		return fmt.Errorf("bench before: %w", err)
	}
	beforePlans, err := bench.Explain(ctx, beforeQueries)
	if err != nil {
		p.Fail(err)
		return fmt.Errorf("explain before: %w", err)
	}
	p.Done()

	// Phase: after
	p = startPhase("Preparing 'after' environment")
	if err := docker.PrepareVersion(ctx, cfg.Migration.AfterVersion); err != nil {
		p.Fail(err)
		return fmt.Errorf("prepare after: %w", err)
	}
	if err := bench.ReadinessCheck(ctx, afterQueries); err != nil {
		p.Fail(err)
		return fmt.Errorf("after readiness: %w", err)
	}
	p.Done()

	p = startPhase("Benchmarking 'after' " + benchLabel)
	afterStats, err := bench.RunRepeats(
		ctx,
		afterQueries,
		uint(cfg.Benchmark.Repeats),
		uint(cfg.Benchmark.Iterations),
		uint(cfg.Benchmark.Concurrency),
		uint(cfg.Benchmark.WarmupIterations),
	)
	if err != nil {
		p.Fail(err)
		return fmt.Errorf("bench after: %w", err)
	}
	afterPlans, err := bench.Explain(ctx, afterQueries)
	if err != nil {
		p.Fail(err)
		return fmt.Errorf("explain after: %w", err)
	}
	p.Done()

	// Analyze
	diffs, err := bench.DiffPlans(beforeQueries, beforePlans, afterQueries, afterPlans)
	if err != nil {
		return fmt.Errorf("diff plans: %w", err)
	}

	speedups := make([]float64, len(beforeStats))
	for i := range beforeStats {
		if afterStats[i].P95 > 0 {
			speedups[i] = float64(beforeStats[i].P95) / float64(afterStats[i].P95)
		}
	}

	data := pgcompare.ReportData{
		GeneratedAt:      time.Now(),
		Iterations:       cfg.Benchmark.Iterations,
		WarmupIterations: cfg.Benchmark.WarmupIterations,
		Concurrency:      cfg.Benchmark.Concurrency,
		Repeats:          cfg.Benchmark.Repeats,
		Speedups:         speedups,
		Before: &pgcompare.BenchResult{
			Phase: "before",
			Stats: beforeStats,
			Plans: beforePlans,
		},
		After: &pgcompare.BenchResult{
			Phase: "after",
			Stats: afterStats,
			Plans: afterPlans,
		},
		Diffs:       diffs,
		Description: cfg.Report.Description,
	}

	p = startPhase("Generating report")
	if err := pgcompare.Generate(data, outPath); err != nil {
		p.Fail(err)
		return fmt.Errorf("generate report: %w", err)
	}
	p.Done()

	printSummary(data, outPath, time.Since(startAll))
	fmt.Println(outPath)
	return nil
}
