package pgcompare

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(cfg *Config)
		wantErr string
	}{
		{
			name: "all fields present",
		},
		{
			name: "missing before version",
			mutate: func(cfg *Config) {
				cfg.Migration.BeforeVersion = ""
			},
			wantErr: "migration.before_version",
		},
		{
			name: "missing env var",
			mutate: func(cfg *Config) {
				cfg.Migration.EnvVar = ""
			},
			wantErr: "migration.env_var",
		},
		{
			name: "missing after version",
			mutate: func(cfg *Config) {
				cfg.Migration.AfterVersion = ""
			},
			wantErr: "migration.after_version",
		},
		{
			name: "missing setup command",
			mutate: func(cfg *Config) {
				cfg.Setup.Command = ""
			},
			wantErr: "setup.command",
		},
		{
			name: "missing before queries",
			mutate: func(cfg *Config) {
				cfg.Benchmark.BeforeQueries = ""
			},
			wantErr: "benchmark.before_queries",
		},
		{
			name: "missing after queries",
			mutate: func(cfg *Config) {
				cfg.Benchmark.AfterQueries = ""
			},
			wantErr: "benchmark.after_queries",
		},
		{
			name: "iterations zero",
			mutate: func(cfg *Config) {
				cfg.Benchmark.Iterations = 0
			},
			wantErr: "benchmark.iterations",
		},
		{
			name: "concurrency zero",
			mutate: func(cfg *Config) {
				cfg.Benchmark.Concurrency = 0
			},
			wantErr: "benchmark.concurrency",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfigForTest()
			if tt.mutate != nil {
				tt.mutate(&cfg)
			}

			err := cfg.validate()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestBuildDSN(t *testing.T) {
	tests := []struct {
		name string
		port string
		want string
	}{
		{
			name: "all vars present",
			port: "9999",
			want: "postgres://u:p@localhost:9999/d",
		},
		{
			name: "default port",
			port: "",
			want: "postgres://u:p@localhost:5432/d",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("POSTGRES_USER", "u")
			t.Setenv("POSTGRES_PASSWORD", "p")
			t.Setenv("POSTGRES_DB", "d")
			t.Setenv("POSTGRES_PORT", tt.port)

			assert.Equal(t, tt.want, buildDSN())
		})
	}
}

func TestLoadConfig(t *testing.T) {
	t.Setenv("POSTGRES_USER", "u")
	t.Setenv("POSTGRES_PASSWORD", "p")
	t.Setenv("POSTGRES_DB", "d")
	t.Setenv("POSTGRES_PORT", "9999")

	t.Run("valid config", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "pgcompare.yaml")

		require.NoError(t, os.WriteFile(configPath, []byte(validYAMLForTest()), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".env"), []byte(validEnvForTest()), 0o644))

		cfg, err := LoadConfig(configPath)
		require.NoError(t, err)
		require.NotNil(t, cfg)

		assert.Equal(t, tmpDir, cfg.ProjectDir)
		assert.Equal(t, "1", cfg.Migration.BeforeVersion)
		assert.Equal(t, "2", cfg.Migration.AfterVersion)
		assert.Equal(t, "postgres://u:p@localhost:9999/d", cfg.DSN)
		require.Len(t, cfg.Report.Description, 1)
		assert.Equal(t, "q1", cfg.Report.Description[0].Query)
	})

	t.Run("missing env file", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "pgcompare.yaml")

		require.NoError(t, os.WriteFile(configPath, []byte(validYAMLForTest()), 0o644))

		cfg, err := LoadConfig(configPath)
		require.Error(t, err)
		assert.Nil(t, cfg)
		assert.Contains(t, err.Error(), "load .env")
	})

	t.Run("invalid yaml", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "pgcompare.yaml")

		require.NoError(t, os.WriteFile(configPath, []byte("migration:\n  before_version: ["), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".env"), []byte(validEnvForTest()), 0o644))

		cfg, err := LoadConfig(configPath)
		require.Error(t, err)
		assert.Nil(t, cfg)
		assert.Contains(t, err.Error(), "parse config")
	})

	t.Run("file not found", func(t *testing.T) {
		cfg, err := LoadConfig(filepath.Join(t.TempDir(), "missing.yaml"))
		require.Error(t, err)
		assert.Nil(t, cfg)
	})
}

func TestNormalizeMigrationEnvVar(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string returns default",
			input: "",
			want:  defaultMigrationVersionEnv,
		},
		{
			name:  "whitespace only returns default",
			input: "   ",
			want:  defaultMigrationVersionEnv,
		},
		{
			name:  "custom value preserved",
			input: "MY_VERSION",
			want:  "MY_VERSION",
		},
		{
			name:  "custom value trimmed",
			input: "  MY_VERSION  ",
			want:  "MY_VERSION",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeMigrationEnvVar(tt.input))
		})
	}
}

func validConfigForTest() Config {
	var cfg Config
	cfg.Migration.EnvVar = defaultMigrationVersionEnv
	cfg.Migration.BeforeVersion = "1"
	cfg.Migration.AfterVersion = "2"
	cfg.Setup.Command = "echo setup"
	cfg.Benchmark.BeforeQueries = "before.sql"
	cfg.Benchmark.AfterQueries = "after.sql"
	cfg.Benchmark.Iterations = 10
	cfg.Benchmark.Concurrency = 2
	return cfg
}

func validYAMLForTest() string {
	return `migration:
  env_var: "MIGRATION_VERSION"
  before_version: "1"
  after_version: "2"
setup:
  command: "echo setup"
benchmark:
  before_queries: "before.sql"
  after_queries: "after.sql"
  iterations: 10
  concurrency: 2
report:
  description:
    - query: "q1"
      what: "users lookup"
      changes: "CREATE INDEX idx_users_name ON users(name);"
      expected: "faster scan"
`
}

func validEnvForTest() string {
	return "POSTGRES_USER=u\nPOSTGRES_PASSWORD=p\nPOSTGRES_DB=d\nPOSTGRES_PORT=9999\n"
}
