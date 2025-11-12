package types

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDuration_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		duration Duration
		want     string
	}{
		{
			name:     "zero duration",
			duration: Duration(0),
			want:     `"0s"`,
		},
		{
			name:     "seconds",
			duration: Duration(10 * time.Second),
			want:     `"10s"`,
		},
		{
			name:     "minutes",
			duration: Duration(5 * time.Minute),
			want:     `"5m0s"`,
		},
		{
			name:     "hours",
			duration: Duration(2 * time.Hour),
			want:     `"2h0m0s"`,
		},
		{
			name:     "mixed",
			duration: Duration(1*time.Hour + 30*time.Minute + 45*time.Second),
			want:     `"1h30m45s"`,
		},
		{
			name:     "milliseconds",
			duration: Duration(500 * time.Millisecond),
			want:     `"500ms"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.duration)
			if err != nil {
				t.Fatalf("MarshalJSON() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("MarshalJSON() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestDuration_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Duration
		wantErr bool
	}{
		{
			name:  "zero duration",
			input: `"0s"`,
			want:  Duration(0),
		},
		{
			name:  "empty string",
			input: `""`,
			want:  Duration(0),
		},
		{
			name:  "seconds",
			input: `"10s"`,
			want:  Duration(10 * time.Second),
		},
		{
			name:  "minutes",
			input: `"5m"`,
			want:  Duration(5 * time.Minute),
		},
		{
			name:  "hours",
			input: `"2h"`,
			want:  Duration(2 * time.Hour),
		},
		{
			name:  "mixed",
			input: `"1h30m45s"`,
			want:  Duration(1*time.Hour + 30*time.Minute + 45*time.Second),
		},
		{
			name:  "milliseconds",
			input: `"500ms"`,
			want:  Duration(500 * time.Millisecond),
		},
		{
			name:  "microseconds",
			input: `"100us"`,
			want:  Duration(100 * time.Microsecond),
		},
		{
			name:    "invalid format",
			input:   `"invalid"`,
			wantErr: true,
		},
		{
			name:    "invalid json",
			input:   `not-json`,
			wantErr: true,
		},
		{
			name:    "negative duration",
			input:   `"-5s"`,
			want:    Duration(-5 * time.Second),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Duration
			err := json.Unmarshal([]byte(tt.input), &got)
			if (err != nil) != tt.wantErr {
				t.Fatalf("UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("UnmarshalJSON() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDuration_String(t *testing.T) {
	tests := []struct {
		name     string
		duration Duration
		want     string
	}{
		{
			name:     "zero",
			duration: Duration(0),
			want:     "0s",
		},
		{
			name:     "seconds",
			duration: Duration(10 * time.Second),
			want:     "10s",
		},
		{
			name:     "minutes",
			duration: Duration(5 * time.Minute),
			want:     "5m0s",
		},
		{
			name:     "hours",
			duration: Duration(2 * time.Hour),
			want:     "2h0m0s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.duration.String(); got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDuration_IsZero(t *testing.T) {
	tests := []struct {
		name     string
		duration Duration
		want     bool
	}{
		{
			name:     "zero",
			duration: Duration(0),
			want:     true,
		},
		{
			name:     "non-zero",
			duration: Duration(10 * time.Second),
			want:     false,
		},
		{
			name:     "negative",
			duration: Duration(-5 * time.Second),
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.duration == 0; got != tt.want {
				t.Errorf("IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDuration_Std(t *testing.T) {
	tests := []struct {
		name     string
		duration Duration
		want     time.Duration
	}{
		{
			name:     "zero",
			duration: Duration(0),
			want:     0,
		},
		{
			name:     "seconds",
			duration: Duration(10 * time.Second),
			want:     10 * time.Second,
		},
		{
			name:     "complex",
			duration: Duration(1*time.Hour + 30*time.Minute + 45*time.Second),
			want:     1*time.Hour + 30*time.Minute + 45*time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.duration; got != tt.want {
				t.Errorf("Std() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDuration_RoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		duration Duration
	}{
		{
			name:     "zero",
			duration: Duration(0),
		},
		{
			name:     "seconds",
			duration: Duration(10 * time.Second),
		},
		{
			name:     "complex",
			duration: Duration(1*time.Hour + 30*time.Minute + 45*time.Second),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.duration)
			if err != nil {
				t.Fatalf("Marshal error = %v", err)
			}

			var got Duration
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal error = %v", err)
			}

			if got != tt.duration {
				t.Errorf("Round trip failed: got %v, want %v", got, tt.duration)
			}
		})
	}
}

func TestDuration_InStruct(t *testing.T) {
	type Config struct {
		Timeout Duration `json:"timeout"`
		Delay   Duration `json:"delay"`
	}

	tests := []struct {
		name    string
		input   string
		want    Config
		wantErr bool
	}{
		{
			name:  "valid config",
			input: `{"timeout":"10s","delay":"5m"}`,
			want: Config{
				Timeout: Duration(10 * time.Second),
				Delay:   Duration(5 * time.Minute),
			},
		},
		{
			name:  "empty values",
			input: `{"timeout":"","delay":""}`,
			want: Config{
				Timeout: Duration(0),
				Delay:   Duration(0),
			},
		},
		{
			name:    "invalid timeout",
			input:   `{"timeout":"invalid","delay":"5m"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Config
			err := json.Unmarshal([]byte(tt.input), &got)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("Unmarshal() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
