package pgcompare

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnvWithOverride(t *testing.T) {
	tests := []struct {
		name  string
		env   []string
		key   string
		value string
		want  []string
	}{
		{
			name:  "replace existing",
			env:   []string{"A=1", "B=2"},
			key:   "A",
			value: "99",
			want:  []string{"A=99", "B=2"},
		},
		{
			name:  "add missing",
			env:   []string{"A=1"},
			key:   "B",
			value: "2",
			want:  []string{"A=1", "B=2"},
		},
		{
			name:  "empty env",
			env:   nil,
			key:   "A",
			value: "1",
			want:  []string{"A=1"},
		},
		{
			name:  "entry without equals sign is skipped",
			env:   []string{"BADENTRY", "A=1"},
			key:   "A",
			value: "99",
			want:  []string{"BADENTRY", "A=99"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, envWithOverride(tt.env, tt.key, tt.value))
		})
	}
}

func TestBuildSetupCommand(t *testing.T) {
	tests := []struct {
		name       string
		command    string
		composeCmd []string
		want       string
	}{
		{
			name:       "replace $DOCKER_COMPOSE",
			command:    "$DOCKER_COMPOSE up",
			composeCmd: []string{"docker", "compose"},
			want:       "docker compose up",
		},
		{
			name:       "replace ${DOCKER_COMPOSE}",
			command:    "${DOCKER_COMPOSE} up",
			composeCmd: []string{"docker", "compose"},
			want:       "docker compose up",
		},
		{
			name:       "without placeholder",
			command:    "echo hello",
			composeCmd: []string{"docker", "compose"},
			want:       "echo hello",
		},
		{
			name:       "docker compose v1",
			command:    "$DOCKER_COMPOSE up",
			composeCmd: []string{"docker-compose"},
			want:       "docker-compose up",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.Setup.Command = tt.command

			d := &dockerComparator{
				cfg:        cfg,
				composeCmd: tt.composeCmd,
			}
			assert.Equal(t, tt.want, d.buildSetupCommand())
		})
	}
}
