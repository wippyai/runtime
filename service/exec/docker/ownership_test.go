// SPDX-License-Identifier: MPL-2.0
package docker

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
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
			process := proc.(*Process)
			process.startTimeout = 25 * time.Millisecond
			err = process.Start()
			require.Error(t, err)
			if deadline {
				require.ErrorContains(t, err, "context deadline exceeded")
			} else {
				require.ErrorContains(t, err, "daemon create refused")
			}
			require.ErrorContains(t, err, "create container")
			require.Equal(t, map[string]string{"bee.owner": "bee", "bee.node_id": "node-1", "bee.state_id": "state-1", "bee.attempt_id": "attempt-1"}, <-received)
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
	for _, value := range []string{"", "bad\x00value", string(make([]byte, 4097))} {
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
