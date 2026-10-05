// SPDX-License-Identifier: MPL-2.0
package docker

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	execapi "github.com/wippyai/runtime/api/service/exec"
	"go.uber.org/zap"
)

func TestOwnershipLabelsExistOnFailedCreate(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(map[bool]string{false: "daemon error", true: "deadline"}[deadline], func(t *testing.T) {
			received := make(chan map[string]string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/_ping" {
					w.Header().Set("API-Version", "1.44")
					return
				}
				if r.Method != "POST" {
					t.Errorf("unexpected operation: %s %s", r.Method, r.URL.Path)
					return
				}
				var body struct{ Labels map[string]string }
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Errorf("invalid container create request: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				received <- body.Labels
				if deadline {
					<-r.Context().Done()
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, `{"message":"daemon create refused"}`)
			}))
			defer server.Close()
			var config execapi.DockerExecutorConfig
			require.NoError(t, json.Unmarshal([]byte(`{"image":"fixture","labels_from_env":{"bee.owner":"OWNER","bee.node_id":"NODE","bee.state_id":"STATE","bee.attempt_id":"ATTEMPT"}}`), &config))
			config.Host = server.URL
			executor, err := NewDockerExecutor(zap.NewNop(), &config)
			require.NoError(t, err)
			defer executor.Close()
			proc, err := executor.NewProcess("true", execapi.ProcessOptions{Env: map[string]string{"OWNER": "bee", "NODE": "node-1", "STATE": "state-1", "ATTEMPT": "attempt-1"}})
			require.NoError(t, err)
			var process *Process
			switch selected := proc.(type) {
			case *Process:
				process = selected
			default:
				t.Fatalf("unexpected Docker process %T", proc)
			}
			process.startTimeout = 250 * time.Millisecond
			err = process.Start()
			require.Error(t, err)
			if deadline {
				require.ErrorContains(t, err, "context deadline exceeded")
			} else {
				require.ErrorContains(t, err, "daemon create refused")
			}
			require.ErrorContains(t, err, "create container")
			select {
			case labels := <-received:
				require.Equal(t, map[string]string{"bee.owner": "bee", "bee.node_id": "node-1", "bee.state_id": "state-1", "bee.attempt_id": "attempt-1"}, labels)
			case <-time.After(time.Second):
				t.Fatal("container create request did not reach the daemon")
			}
			require.False(t, process.started)
			require.Empty(t, process.containerID)
		})
	}
}

func TestMissingOwnershipSourceRefusesBeforeCreate(t *testing.T) {
	var config execapi.DockerExecutorConfig
	require.NoError(t, json.Unmarshal([]byte(`{"image":"fixture","labels_from_env":{"owner":"OWNER"}}`), &config))
	executor, err := NewDockerExecutor(zap.NewNop(), &config)
	require.NoError(t, err)
	defer executor.Close()
	_, err = executor.NewProcess("true", execapi.ProcessOptions{})
	require.ErrorContains(t, err, "label source OWNER")
	var invalid apierror.Error
	require.ErrorAs(t, err, &invalid)
	require.Equal(t, apierror.Invalid, invalid.Kind())
	require.Equal(t, apierror.False, invalid.Retryable())
	for _, value := range []string{"", "bad\x00value", strings.Repeat("x", 4097)} {
		_, err = executor.NewProcess("true", execapi.ProcessOptions{Env: map[string]string{"OWNER": value}})
		require.ErrorContains(t, err, "label source OWNER")
	}
}

func TestOwnershipLabelsFreezeHostMappingAndProcessValues(t *testing.T) {
	config := &execapi.DockerExecutorConfig{Image: "fixture", LabelsFromEnv: map[string]string{"owner": "OWNER"}}
	executor, err := NewDockerExecutor(zap.NewNop(), config)
	require.NoError(t, err)
	defer executor.Close()
	config.LabelsFromEnv["owner"] = "FOREIGN"
	for _, pty := range []*execapi.PTYOptions{nil, {Width: 80, Height: 24}} {
		env := map[string]string{"OWNER": "owned"}
		proc, err := executor.NewProcess("true", execapi.ProcessOptions{Env: env, PTY: pty})
		require.NoError(t, err)
		env["OWNER"] = "changed"
		var labels map[string]string
		switch process := proc.(type) {
		case *Process:
			labels = process.labels
		case *ptyProcess:
			labels = process.labels
		default:
			t.Fatalf("unexpected Docker process %T", proc)
		}
		require.Equal(t, map[string]string{"owner": "owned"}, labels)
	}
}

func TestOwnershipValuesRequireExplicitEnv(t *testing.T) {
	t.Setenv("OWNER", "ambient")
	cfg := &execapi.DockerExecutorConfig{
		Image: "fixture", LabelsFromEnv: map[string]string{"owner": "OWNER"},
		DefaultEnv: map[string]string{"OWNER": "default"},
	}
	executor, err := NewDockerExecutor(zap.NewNop(), cfg)
	require.NoError(t, err)
	defer executor.Close()
	_, err = executor.NewProcess("true", execapi.ProcessOptions{})
	require.ErrorContains(t, err, "label source OWNER")
	value := strings.Repeat("v", 4096)
	proc, err := executor.NewProcess("true", execapi.ProcessOptions{Env: map[string]string{"OWNER": value}})
	require.NoError(t, err)
	require.Equal(t, value, proc.(*Process).labels["owner"])

	cfg.LabelsFromEnv = nil
	unlabelled, err := NewDockerExecutor(zap.NewNop(), cfg)
	require.NoError(t, err)
	defer unlabelled.Close()
	proc, err = unlabelled.NewProcess("true", execapi.ProcessOptions{})
	require.NoError(t, err)
	require.Empty(t, proc.(*Process).labels)
}
