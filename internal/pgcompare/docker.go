package pgcompare

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

const (
	defaultMigrationVersionEnv = "MIGRATION_VERSION"
	defaultDockerComposeEnv    = "DOCKER_COMPOSE"
)

type dockerComparator struct {
	log        *slog.Logger
	cfg        *Config
	composeCmd []string
}

func NewDockerComparator(log *slog.Logger, cfg *Config) (*dockerComparator, error) {
	dockerCompose, err := detectDockerCompose()
	if err != nil {
		return nil, fmt.Errorf("detect docker compose: %w", err)
	}
	return &dockerComparator{log: log, composeCmd: dockerCompose, cfg: cfg}, nil
}

func (d *dockerComparator) Cleanup(ctx context.Context) error {
	args := append(append([]string{}, d.composeCmd...), "down", "-v")
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)

	cmd.Dir = d.cfg.ProjectDir
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	d.log.Info("cleanup docker environment", "cmd", strings.Join(args, " "), "dir", cmd.Dir)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cleanup docker environment: %w", err)
	}

	return nil
}

func (d *dockerComparator) PrepareVersion(ctx context.Context, version string) error {
	d.log.Info("prepare docker version", "version", version)

	if err := d.Cleanup(ctx); err != nil {
		return fmt.Errorf("prepare docker version: %w", err)
	}

	if err := d.runSetup(ctx, version); err != nil {
		return fmt.Errorf("run docker setup: %w", err)
	}

	return nil
}

func (d *dockerComparator) runSetup(ctx context.Context, version string) error {
	command := d.buildSetupCommand()

	cmd := shellCommand(ctx, command)
	cmd.Dir = d.cfg.ProjectDir

	env := os.Environ()
	env = envWithOverride(env, defaultMigrationVersionEnv, version)
	env = envWithOverride(env, defaultDockerComposeEnv, strings.Join(d.composeCmd, " "))
	cmd.Env = env

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run setup for migration version %s: %w", version, err)
	}

	return nil
}

func (d *dockerComparator) buildSetupCommand() string {
	command := d.cfg.Setup.Command
	compose := strings.Join(d.composeCmd, " ")

	command = strings.ReplaceAll(command, fmt.Sprintf("$%s", defaultDockerComposeEnv), compose)
	command = strings.ReplaceAll(command, fmt.Sprintf("${%s}", defaultDockerComposeEnv), compose)

	return command
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
