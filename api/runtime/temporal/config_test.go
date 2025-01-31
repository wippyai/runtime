package temporal

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestClientConfig_TLS(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *ClientConfig
		wantErr  bool
	}{
		{
			name: "valid config with TLS",
			input: `{
				"address": "localhost:7233",
				"namespace": "default",
				"tls": {
					"key": "key.pem",
					"cert": "cert.pem",
					"root_ca": "ca.pem",
					"client_auth_type": "require_and_verify_client_cert",
					"server_name": "temporal",
					"use_h2c": false
				},
				"cache_size": 1000
			}`,
			expected: &ClientConfig{
				Address:   "localhost:7233",
				Namespace: "default",
				TLS: &TLSConfig{
					Key:        "key.pem",
					Cert:       "cert.pem",
					RootCA:     "ca.pem",
					AuthType:   RequireAndVerifyClientCert,
					ServerName: "temporal",
					UseH2C:     false,
				},
				CacheSize: 1000,
			},
			wantErr: false,
		},
		{
			name: "valid config without TLS",
			input: `{
				"address": "localhost:7233",
				"namespace": "default",
				"cache_size": 1000
			}`,
			expected: &ClientConfig{
				Address:   "localhost:7233",
				Namespace: "default",
				CacheSize: 1000,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got ClientConfig
			err := json.Unmarshal([]byte(tt.input), &got)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, &got)

			// Test marshaling
			marshaled, err := json.Marshal(&got)
			assert.NoError(t, err)

			var unmarshaled ClientConfig
			err = json.Unmarshal(marshaled, &unmarshaled)
			assert.NoError(t, err)
			assert.Equal(t, got, unmarshaled)
		})
	}
}

func TestTaskQueueConfig_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *TaskQueueConfig
		wantErr  bool
	}{
		{
			name: "valid config with all fields",
			input: `{
				"client": "test-client",
				"task_queue": "test-queue",
				"max_concurrent_activity_execution": 10,
				"worker_activities_per_second": 5.0,
				"sticky_schedule_timeout": "1m",
				"worker_stop_timeout": "30s",
				"max_heartbeat_throttle_interval": "5s",
				"default_heartbeat_throttle_interval": "1s",
				"deadlock_detection_timeout": "1m"
			}`,
			expected: &TaskQueueConfig{
				Client:                           "test-client",
				TaskQueue:                        "test-queue",
				MaxConcurrentActivityExecution:   10,
				WorkerActivitiesPerSecond:        5.0,
				StickyScheduleTimeout:            time.Minute,
				WorkerStopTimeout:                30 * time.Second,
				MaxHeartbeatThrottleInterval:     5 * time.Second,
				DefaultHeartbeatThrottleInterval: time.Second,
				DeadlockDetectionTimeout:         time.Minute,
			},
			wantErr: false,
		},
		{
			name: "invalid duration format",
			input: `{
				"meta": {"name": "test-queue"},
				"client": "test-client",
				"task_queue": "test-queue",
				"sticky_schedule_timeout": "invalid"
			}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got TaskQueueConfig
			err := json.Unmarshal([]byte(tt.input), &got)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, &got)

			// Test marshaling
			marshaled, err := json.Marshal(&got)
			assert.NoError(t, err)

			var unmarshaled TaskQueueConfig
			err = json.Unmarshal(marshaled, &unmarshaled)
			assert.NoError(t, err)
			assert.Equal(t, got, unmarshaled)
		})
	}
}

func TestTaskQueueConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  TaskQueueConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: TaskQueueConfig{
				Client:    "test-client",
				TaskQueue: "test-queue",
			},
			wantErr: false,
		},
		{
			name: "missing client",
			config: TaskQueueConfig{
				TaskQueue: "test-queue",
			},
			wantErr: true,
		},
		{
			name: "missing task queue",
			config: TaskQueueConfig{
				Client: "test-client",
			},
			wantErr: true,
		},
		{
			name: "negative concurrent activity execution",
			config: TaskQueueConfig{
				Client:                         "test-client",
				TaskQueue:                      "test-queue",
				MaxConcurrentActivityExecution: -1,
			},
			wantErr: true,
		},
		{
			name: "negative worker activities per second",
			config: TaskQueueConfig{
				Client:                    "test-client",
				TaskQueue:                 "test-queue",
				WorkerActivitiesPerSecond: -1.0,
			},
			wantErr: true,
		},
		{
			name: "negative sticky schedule timeout",
			config: TaskQueueConfig{
				Client:                "test-client",
				TaskQueue:             "test-queue",
				StickyScheduleTimeout: -time.Second,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTaskQueueConfig_ToWorkerOptions(t *testing.T) {
	config := TaskQueueConfig{
		MaxConcurrentActivityExecution:   10,
		WorkerActivitiesPerSecond:        5.0,
		MaxConcurrentLocalActivity:       8,
		WorkerLocalActivitiesPerSecond:   4.0,
		TaskQueueActivitiesPerSecond:     3.0,
		MaxConcurrentActivityPollers:     6,
		MaxConcurrentWorkflowExecution:   15,
		MaxConcurrentWorkflowPollers:     7,
		StickyScheduleTimeout:            time.Minute,
		EnableLoggingReplay:              true,
		WorkerStopTimeout:                30 * time.Second,
		MaxHeartbeatThrottleInterval:     5 * time.Second,
		DefaultHeartbeatThrottleInterval: time.Second,
		EnableSessionWorker:              true,
		MaxConcurrentSessionExecution:    12,
		DisableWorkflowWorker:            false,
		LocalActivityWorkerOnly:          false,
		DeadlockDetectionTimeout:         2 * time.Minute,
		DisableEagerActivities:           false,
		MaxConcurrentEagerActivities:     20,
		DisableRegistrationAliasing:      false,
	}

	options := config.ToWorkerOptions()

	assert.Equal(t, config.MaxConcurrentActivityExecution, options.MaxConcurrentActivityExecutionSize)
	assert.Equal(t, config.WorkerActivitiesPerSecond, options.WorkerActivitiesPerSecond)
	assert.Equal(t, config.MaxConcurrentLocalActivity, options.MaxConcurrentLocalActivityExecutionSize)
	assert.Equal(t, config.WorkerLocalActivitiesPerSecond, options.WorkerLocalActivitiesPerSecond)
	assert.Equal(t, config.TaskQueueActivitiesPerSecond, options.TaskQueueActivitiesPerSecond)
	assert.Equal(t, config.MaxConcurrentActivityPollers, options.MaxConcurrentActivityTaskPollers)
	assert.Equal(t, config.MaxConcurrentWorkflowExecution, options.MaxConcurrentWorkflowTaskExecutionSize)
	assert.Equal(t, config.MaxConcurrentWorkflowPollers, options.MaxConcurrentWorkflowTaskPollers)
	assert.Equal(t, config.EnableLoggingReplay, options.EnableLoggingInReplay)
	assert.Equal(t, config.StickyScheduleTimeout, options.StickyScheduleToStartTimeout)
	assert.Equal(t, config.WorkerStopTimeout, options.WorkerStopTimeout)
	assert.Equal(t, config.MaxHeartbeatThrottleInterval, options.MaxHeartbeatThrottleInterval)
	assert.Equal(t, config.DefaultHeartbeatThrottleInterval, options.DefaultHeartbeatThrottleInterval)
	assert.Equal(t, config.EnableSessionWorker, options.EnableSessionWorker)
	assert.Equal(t, config.MaxConcurrentSessionExecution, options.MaxConcurrentSessionExecutionSize)
	assert.Equal(t, config.DisableWorkflowWorker, options.DisableWorkflowWorker)
	assert.Equal(t, config.LocalActivityWorkerOnly, options.LocalActivityWorkerOnly)
	assert.Equal(t, config.DeadlockDetectionTimeout, options.DeadlockDetectionTimeout)
	assert.Equal(t, config.DisableEagerActivities, options.DisableEagerActivities)
	assert.Equal(t, config.MaxConcurrentEagerActivities, options.MaxConcurrentEagerActivityExecutionSize)
	assert.Equal(t, config.DisableRegistrationAliasing, options.DisableRegistrationAliasing)
}
