package cmd

import (
	"context"
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReadSecrets(t *testing.T) {
	secrets := map[string]string{}
	ret := readEnvsEx(path.Join("testdata", "secrets.yml"), secrets, true)
	assert.True(t, ret)
	assert.Equal(t, `line1
line2
line3
`, secrets["MYSECRET"])
}

func TestReadEnv(t *testing.T) {
	secrets := map[string]string{}
	ret := readEnvs(path.Join("testdata", "secrets.yml"), secrets)
	assert.True(t, ret)
	assert.Equal(t, `line1
line2
line3
`, secrets["mysecret"])
}

func TestListOptions(t *testing.T) {
	rootCmd := createRootCommand(context.Background(), &Input{}, "")
	err := newRunCommand(context.Background(), &Input{
		listOptions: true,
	})(rootCmd, []string{})
	assert.NoError(t, err)
}

func TestRun(t *testing.T) {
	rootCmd := createRootCommand(context.Background(), &Input{}, "")
	err := newRunCommand(context.Background(), &Input{
		platforms:     []string{"ubuntu-latest=node:16-buster-slim"},
		workdir:       "../pkg/runner/testdata/",
		workflowsPath: "./basic/push.yml",
	})(rootCmd, []string{})
	assert.NoError(t, err)
}

func TestRunPush(t *testing.T) {
	rootCmd := createRootCommand(context.Background(), &Input{}, "")
	err := newRunCommand(context.Background(), &Input{
		platforms:     []string{"ubuntu-latest=node:16-buster-slim"},
		workdir:       "../pkg/runner/testdata/",
		workflowsPath: "./basic/push.yml",
	})(rootCmd, []string{"push"})
	assert.NoError(t, err)
}

func TestRunPushJsonLogger(t *testing.T) {
	rootCmd := createRootCommand(context.Background(), &Input{}, "")
	err := newRunCommand(context.Background(), &Input{
		platforms:     []string{"ubuntu-latest=node:16-buster-slim"},
		workdir:       "../pkg/runner/testdata/",
		workflowsPath: "./basic/push.yml",
		jsonLogger:    true,
	})(rootCmd, []string{"push"})
	assert.NoError(t, err)
}

func TestFlags(t *testing.T) {
	for _, f := range []string{"graph", "list", "bug-report", "man-page"} {
		t.Run("TestFlag-"+f, func(t *testing.T) {
			rootCmd := createRootCommand(context.Background(), &Input{}, "")
			err := rootCmd.Flags().Set(f, "true")
			assert.NoError(t, err)
			err = newRunCommand(context.Background(), &Input{
				platforms:     []string{"ubuntu-latest=node:16-buster-slim"},
				workdir:       "../pkg/runner/testdata/",
				workflowsPath: "./basic/push.yml",
			})(rootCmd, []string{})
			assert.NoError(t, err)
		})
	}
}

func TestReadArgsFile(t *testing.T) {
	tables := []struct {
		path  string
		split bool
		args  []string
		env   map[string]string
	}{
		{
			path:  path.Join("testdata", "simple.actrc"),
			split: true,
			args:  []string{"--container-architecture=linux/amd64", "--action-offline-mode"},
		},
		{
			path:  path.Join("testdata", "env.actrc"),
			split: true,
			env: map[string]string{
				"FAKEPWD": "/fake/test/pwd",
				"FOO":     "foo",
			},
			args: []string{
				"--artifact-server-path", "/fake/test/pwd/.artifacts",
				"--env", "FOO=prefix/foo/suffix",
			},
		},
		{
			path:  path.Join("testdata", "split.actrc"),
			split: true,
			args:  []string{"--container-options", "--volume /foo:/bar --volume /baz:/qux --volume /tmp:/tmp"},
		},
	}
	for _, table := range tables {
		t.Run(table.path, func(t *testing.T) {
			for k, v := range table.env {
				t.Setenv(k, v)
			}
			args := readArgsFile(table.path, table.split)
			assert.Equal(t, table.args, args)
		})
	}
}

func TestGetDefaultNetworkStack(t *testing.T) {
	tests := []struct {
		name     string
		wslEnv   string
		expected string
	}{
		{
			name:     "WSL2 environment",
			wslEnv:   "Ubuntu",
			expected: "tcp4",
		},
		{
			name:     "Non-WSL environment",
			wslEnv:   "",
			expected: "tcp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original the value
			oldVal, wasSet := os.LookupEnv("WSL_DISTRO_NAME")

			if tt.wslEnv != "" {
				os.Setenv("WSL_DISTRO_NAME", tt.wslEnv)
			} else {
				os.Unsetenv("WSL_DISTRO_NAME")
			}

			// Cleanup: restore the original state
			t.Cleanup(func() {
				if wasSet {
					os.Setenv("WSL_DISTRO_NAME", oldVal)
				} else {
					os.Unsetenv("WSL_DISTRO_NAME")
				}
			})

			result := getDefaultNetworkStack()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidateNetworkStack(t *testing.T) {
	tests := []struct {
		name      string
		network   string
		wantError bool
	}{
		{
			name:      "valid tcp",
			network:   "tcp",
			wantError: false,
		},
		{
			name:      "valid tcp4",
			network:   "tcp4",
			wantError: false,
		},
		{
			name:      "valid tcp6",
			network:   "tcp6",
			wantError: false,
		},
		{
			name:      "invalid udp",
			network:   "udp",
			wantError: true,
		},
		{
			name:      "invalid empty",
			network:   "",
			wantError: true,
		},
		{
			name:      "invalid random",
			network:   "invalid",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateNetworkStack(tt.network)
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestParseEnvironmentFlag(t *testing.T) {
	tests := []struct {
		name        string
		flag        string
		wantEnv     string
		wantValue   string
		wantErr     bool
		errContains string
	}{
		{
			name:      "valid flag",
			flag:      "production:KEY=VALUE",
			wantEnv:   "production",
			wantValue: "KEY=VALUE",
			wantErr:   false,
		},
		{
			name:      "case normalization",
			flag:      "Production:KEY=VALUE",
			wantEnv:   "production",
			wantValue: "KEY=VALUE",
			wantErr:   false,
		},
		{
			name:      "mixed case normalization",
			flag:      "STAGING:KEY=VALUE",
			wantEnv:   "staging",
			wantValue: "KEY=VALUE",
			wantErr:   false,
		},
		{
			name:        "missing colon",
			flag:        "productionKEY=VALUE",
			wantErr:     true,
			errContains: "invalid environment flag format",
		},
		{
			name:        "empty environment name",
			flag:        ":KEY=VALUE",
			wantErr:     true,
			errContains: "environment name cannot be empty",
		},
		{
			name:        "empty value",
			flag:        "production:",
			wantErr:     true,
			errContains: "value cannot be empty",
		},
		{
			name:        "environment name too long",
			flag:        string(make([]byte, 256)) + ":KEY=VALUE",
			wantErr:     true,
			errContains: "exceeds 255 characters",
		},
		{
			name:      "value with colon",
			flag:      "production:KEY=VALUE:WITH:COLONS",
			wantEnv:   "production",
			wantValue: "KEY=VALUE:WITH:COLONS",
			wantErr:   false,
		},
		{
			name:      "whitespace trimming",
			flag:      "  production  :  KEY=VALUE  ",
			wantEnv:   "production",
			wantValue: "KEY=VALUE",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, value, err := parseEnvironmentFlag(tt.flag)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantEnv, env)
				assert.Equal(t, tt.wantValue, value)
			}
		})
	}
}

func TestParseKeyValue(t *testing.T) {
	tests := []struct {
		name        string
		kv          string
		wantKey     string
		wantValue   string
		wantErr     bool
		errContains string
	}{
		{
			name:      "valid key-value",
			kv:        "KEY=VALUE",
			wantKey:   "KEY",
			wantValue: "VALUE",
			wantErr:   false,
		},
		{
			name:      "value with equals",
			kv:        "KEY=VALUE=WITH=EQUALS",
			wantKey:   "KEY",
			wantValue: "VALUE=WITH=EQUALS",
			wantErr:   false,
		},
		{
			name:      "empty value",
			kv:        "KEY=",
			wantKey:   "KEY",
			wantValue: "",
			wantErr:   false,
		},
		{
			name:        "missing equals",
			kv:          "KEYVALUE",
			wantErr:     true,
			errContains: "invalid key=value format",
		},
		{
			name:        "empty key",
			kv:          "=VALUE",
			wantErr:     true,
			errContains: "key cannot be empty",
		},
		{
			name:      "whitespace in key trimmed",
			kv:        "  KEY  =VALUE",
			wantKey:   "KEY",
			wantValue: "VALUE",
			wantErr:   false,
		},
		{
			name:      "whitespace in value preserved",
			kv:        "KEY=  VALUE  ",
			wantKey:   "KEY",
			wantValue: "  VALUE  ",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, value, err := parseKeyValue(tt.kv)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantKey, key)
				assert.Equal(t, tt.wantValue, value)
			}
		})
	}
}
>>>>>>> conflict 1 of 1 ends
