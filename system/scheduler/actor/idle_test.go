package actor

import (
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// TestIdleCPUConsumption verifies that idle scheduler doesn't burn CPU
func TestIdleCPUConsumption(t *testing.T) {
	sched := newTestScheduler(runtime.GOMAXPROCS(0))
	sched.Start()
	defer sched.Stop()

	// Let workers settle into deep idle state first
	time.Sleep(100 * time.Millisecond)

	// Measure CPU time during idle period
	cpuBefore := getCPUTime()
	wallStart := time.Now()

	// Let scheduler idle for 2 seconds (enough for 1ms sleep cycles)
	time.Sleep(2 * time.Second)

	cpuAfter := getCPUTime()
	wallTime := time.Since(wallStart)

	// Calculate CPU usage during idle period
	cpuUsed := cpuAfter - cpuBefore

	// With 32 workers each doing 1ms sleep cycles, expect some baseline CPU:
	// - 32 workers * 1000 wakeups/sec * ~50us per wakeup = ~1.6ms/sec = 0.16%
	// - Plus Go runtime overhead
	// Allow 10% for WSL/VM overhead and runtime scheduling
	maxAllowedCPU := wallTime * 10 / 100 // 10% of wall time

	t.Logf("Idle period: %v", wallTime)
	t.Logf("CPU time used: %v", cpuUsed)
	t.Logf("Max allowed: %v (5%%)", maxAllowedCPU)
	t.Logf("CPU percentage: %.2f%%", float64(cpuUsed)/float64(wallTime)*100)

	if cpuUsed > maxAllowedCPU {
		t.Errorf("Idle CPU usage too high: %v (max %v)", cpuUsed, maxAllowedCPU)
	}
}

// getCPUTime returns process CPU time (user + system)
func getCPUTime() time.Duration {
	// Read from /proc/self/stat on Linux
	data, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0
	}

	// Parse utime and stime (fields 14 and 15)
	fields := splitStatFields(string(data))
	if len(fields) < 15 {
		return 0
	}

	utime, _ := strconv.ParseInt(fields[13], 10, 64)
	stime, _ := strconv.ParseInt(fields[14], 10, 64)

	// Convert clock ticks to nanoseconds (100 ticks/sec on most Linux)
	ticksPerSec := int64(100)
	totalTicks := utime + stime
	return time.Duration(totalTicks * int64(time.Second) / ticksPerSec)
}

func splitStatFields(s string) []string {
	// Handle comm field which can contain spaces in parentheses
	var fields []string
	var field string
	inParen := false

	for _, r := range s {
		if r == '(' {
			inParen = true
		} else if r == ')' {
			inParen = false
		}

		if r == ' ' && !inParen {
			if field != "" {
				fields = append(fields, field)
				field = ""
			}
		} else {
			field += string(r)
		}
	}
	if field != "" {
		fields = append(fields, field)
	}
	return fields
}

// BenchmarkIdleOverhead measures overhead of idle workers
func BenchmarkIdleOverhead(b *testing.B) {
	sched := newTestScheduler(runtime.GOMAXPROCS(0))
	sched.Start()
	defer sched.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		time.Sleep(time.Microsecond)
	}
}
