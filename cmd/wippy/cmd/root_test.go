package cmd

import "testing"

func TestParseMemorySize(t *testing.T) {
	tests := []struct {
		input   string
		want    int64
		wantErr bool
	}{
		{"1G", 1 << 30, false},
		{"1g", 1 << 30, false},
		{"1GB", 1 << 30, false},
		{"1gb", 1 << 30, false},
		{"512M", 512 << 20, false},
		{"512m", 512 << 20, false},
		{"512MB", 512 << 20, false},
		{"2048M", 2048 << 20, false},
		{"1024K", 1024 << 10, false},
		{"1T", 1 << 40, false},
		{"1TB", 1 << 40, false},
		{"1073741824", 1073741824, false},
		{"", 0, false},
		{"  1G  ", 1 << 30, false},
		{"invalid", 0, true},
		{"1X", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseMemorySize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseMemorySize(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseMemorySize(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0B"},
		{512, "512B"},
		{1024, "1.0KB"},
		{1536, "1.5KB"},
		{1 << 20, "1.0MB"},
		{512 << 20, "512.0MB"},
		{1 << 30, "1.0GB"},
		{2 << 30, "2.0GB"},
		{1 << 40, "1.0TB"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatBytes(tt.input)
			if got != tt.want {
				t.Errorf("formatBytes(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
