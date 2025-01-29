package temporal

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskQueueConfig_MarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		config  TaskQueueConfig
		wantErr bool
	}{
		{
			name: "full config",
			config: TaskQueueConfig{
				Meta:                             registry.Metadata{"version": "1.0"},
				MaxConcurrentActivityExecution:   100,
				WorkerActivitiesPerSecond:        50.0,
				MaxConcurrentLocalActivity:       50,
				WorkerLocalActivitiesPerSecond:   25.0,
				TaskQueueActivitiesPerSecond:     100.0,
				MaxConcurrentActivityPollers:     2,
				MaxConcurrentWorkflowExecution:   100,
				MaxConcurrentWorkflowPollers:     2,
				StickyScheduleTimeout:            5 * time.Second,
				EnableLoggingReplay:              false,
				WorkerStopTimeout:                30 * time.Second,
				MaxHeartbeatThrottleInterval:     60 * time.Second,
				DefaultHeartbeatThrottleInterval: 30 * time.Second,
				EnableSessionWorker:              false,
				MaxConcurrentSessionExecution:    100,
				DisableWorkflowWorker:            false,
				LocalActivityWorkerOnly:          false,
				DeadlockDetectionTimeout:         1 * time.Second,
				DisableEagerActivities:           false,
				MaxConcurrentEagerActivities:     100,
				DisableRegistrationAliasing:      true,
			},
			wantErr: false,
		},
		{
			name: "minimal config",
			config: TaskQueueConfig{
				Meta: registry.Metadata{},
			},
			wantErr: false,
		},
		{
			name: "config with complex durations",
			config: TaskQueueConfig{
				Meta:                             registry.Metadata{"version": "1.0"},
				StickyScheduleTimeout:            1*time.Hour + 30*time.Minute,
				WorkerStopTimeout:                2*time.Hour + 15*time.Minute,
				MaxHeartbeatThrottleInterval:     45*time.Minute + 30*time.Second,
				DefaultHeartbeatThrottleInterval: 3*time.Hour + 45*time.Minute,
				DeadlockDetectionTimeout:         2*time.Minute + 30*time.Second,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal
			data, err := json.Marshal(&tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Unmarshal and compare
			var decoded TaskQueueConfig
			err = json.Unmarshal(data, &decoded)
			require.NoError(t, err)
			assert.Equal(t, tt.config, decoded)
		})
	}
}

func TestTaskQueueConfig_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		expected TaskQueueConfig
		wantErr  bool
	}{
		{
			name: "valid configuration",
			json: `{
				"meta": {"version": "1.0"},
				"max_concurrent_activity_execution": 100,
				"worker_activities_per_second": 50.0,
				"sticky_schedule_timeout": "5s",
				"worker_stop_timeout": "30s",
				"max_heartbeat_throttle_interval": "60s",
				"default_heartbeat_throttle_interval": "30s",
				"deadlock_detection_timeout": "1s"
			}`,
			expected: TaskQueueConfig{
				Meta:                             registry.Metadata{"version": "1.0"},
				MaxConcurrentActivityExecution:   100,
				WorkerActivitiesPerSecond:        50.0,
				StickyScheduleTimeout:            5 * time.Second,
				WorkerStopTimeout:                30 * time.Second,
				MaxHeartbeatThrottleInterval:     60 * time.Second,
				DefaultHeartbeatThrottleInterval: 30 * time.Second,
				DeadlockDetectionTimeout:         1 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "invalid sticky schedule timeout",
			json: `{
				"sticky_schedule_timeout": "invalid"
			}`,
			wantErr: true,
		},
		{
			name: "invalid worker stop timeout",
			json: `{
				"worker_stop_timeout": "invalid"
			}`,
			wantErr: true,
		},
		{
			name: "invalid heartbeat throttle interval",
			json: `{
				"max_heartbeat_throttle_interval": "invalid"
			}`,
			wantErr: true,
		},
		{
			name: "invalid default heartbeat throttle interval",
			json: `{
				"default_heartbeat_throttle_interval": "invalid"
			}`,
			wantErr: true,
		},
		{
			name: "invalid deadlock detection timeout",
			json: `{
				"deadlock_detection_timeout": "invalid"
			}`,
			wantErr: true,
		},
		{
			name:     "empty object",
			json:     `{}`,
			expected: TaskQueueConfig{},
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var config TaskQueueConfig
			err := json.Unmarshal([]byte(tt.json), &config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, config)
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
				MaxConcurrentActivityExecution:   100,
				WorkerActivitiesPerSecond:        50.0,
				MaxConcurrentLocalActivity:       50,
				StickyScheduleTimeout:            5 * time.Second,
				WorkerStopTimeout:                30 * time.Second,
				MaxHeartbeatThrottleInterval:     60 * time.Second,
				DefaultHeartbeatThrottleInterval: 30 * time.Second,
				DeadlockDetectionTimeout:         1 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "negative max concurrent activity execution",
			config: TaskQueueConfig{
				MaxConcurrentActivityExecution: -1,
			},
			wantErr: true,
		},
		{
			name: "negative worker activities per second",
			config: TaskQueueConfig{
				WorkerActivitiesPerSecond: -1.0,
			},
			wantErr: true,
		},
		{
			name: "negative sticky schedule timeout",
			config: TaskQueueConfig{
				StickyScheduleTimeout: -5 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "negative worker stop timeout",
			config: TaskQueueConfig{
				WorkerStopTimeout: -30 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "negative max heartbeat throttle interval",
			config: TaskQueueConfig{
				MaxHeartbeatThrottleInterval: -60 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "negative default heartbeat throttle interval",
			config: TaskQueueConfig{
				DefaultHeartbeatThrottleInterval: -30 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "negative deadlock detection timeout",
			config: TaskQueueConfig{
				DeadlockDetectionTimeout: -1 * time.Second,
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
		MaxConcurrentActivityExecution:   100,
		WorkerActivitiesPerSecond:        50.0,
		MaxConcurrentLocalActivity:       50,
		WorkerLocalActivitiesPerSecond:   25.0,
		TaskQueueActivitiesPerSecond:     100.0,
		MaxConcurrentActivityPollers:     2,
		MaxConcurrentWorkflowExecution:   100,
		MaxConcurrentWorkflowPollers:     2,
		StickyScheduleTimeout:            5 * time.Second,
		EnableLoggingReplay:              false,
		WorkerStopTimeout:                30 * time.Second,
		MaxHeartbeatThrottleInterval:     60 * time.Second,
		DefaultHeartbeatThrottleInterval: 30 * time.Second,
		EnableSessionWorker:              false,
		MaxConcurrentSessionExecution:    100,
		DisableWorkflowWorker:            false,
		LocalActivityWorkerOnly:          false,
		DeadlockDetectionTimeout:         1 * time.Second,
		DisableEagerActivities:           false,
		MaxConcurrentEagerActivities:     100,
		DisableRegistrationAliasing:      true,
	}

	options := config.ToWorkerOptions()

	// Verify all fields are correctly mapped
	assert.Equal(t, config.MaxConcurrentActivityExecution, options.MaxConcurrentActivityExecutionSize)
	assert.Equal(t, config.WorkerActivitiesPerSecond, options.WorkerActivitiesPerSecond)
	assert.Equal(t, config.MaxConcurrentLocalActivity, options.MaxConcurrentLocalActivityExecutionSize)
	assert.Equal(t, config.WorkerLocalActivitiesPerSecond, options.WorkerLocalActivitiesPerSecond)
	assert.Equal(t, config.TaskQueueActivitiesPerSecond, options.TaskQueueActivitiesPerSecond)
	assert.Equal(t, config.MaxConcurrentActivityPollers, options.MaxConcurrentActivityTaskPollers)
	assert.Equal(t, config.MaxConcurrentWorkflowExecution, options.MaxConcurrentWorkflowTaskExecutionSize)
	assert.Equal(t, config.MaxConcurrentWorkflowPollers, options.MaxConcurrentWorkflowTaskPollers)
	assert.Equal(t, config.StickyScheduleTimeout, options.StickyScheduleToStartTimeout)
	assert.Equal(t, config.EnableLoggingReplay, options.EnableLoggingInReplay)
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
