package engine

import (
	"runtime"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"go.uber.org/zap"
)

// TestOldCVMMemoryProfile creates many CVMs and reports memory stats for comparison.
func TestOldCVMMemoryProfile(t *testing.T) {
	logger := zap.NewNop()
	counts := []int{100, 500, 1000}

	for _, count := range counts {
		runtime.GC()
		var m1 runtime.MemStats
		runtime.ReadMemStats(&m1)

		cvms := make([]*CoroutineVM, count)
		for i := 0; i < count; i++ {
			vm, err := NewCVM(logger)
			if err != nil {
				t.Fatal(err)
			}

			ctx := ctxapi.NewRootContext()
			err = vm.StartString(ctx, `return 1`, "test")
			if err != nil {
				t.Fatal(err)
			}
			vm.Step()
			cvms[i] = vm
		}

		runtime.GC()
		var m2 runtime.MemStats
		runtime.ReadMemStats(&m2)

		bytesUsed := m2.Alloc - m1.Alloc
		bytesPerCVM := bytesUsed / uint64(count)

		t.Logf("%d CVMs: total=%dKB, per-CVM=%dKB",
			count, bytesUsed/1024, bytesPerCVM/1024)

		processesIn1GB := (1024 * 1024 * 1024) / bytesPerCVM
		t.Logf("  -> estimated %d CVMs in 1GB RAM", processesIn1GB)

		for _, vm := range cvms {
			vm.Close()
		}
	}
}

// BenchmarkOldCVMCreate measures CVM creation overhead.
func BenchmarkOldCVMCreate(b *testing.B) {
	logger := zap.NewNop()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		vm, err := NewCVM(logger)
		if err != nil {
			b.Fatal(err)
		}

		ctx := ctxapi.NewRootContext()
		err = vm.StartString(ctx, `return 1`, "test")
		if err != nil {
			b.Fatal(err)
		}
		vm.Close()
	}
}

// BenchmarkOldCVMStep measures CVM step overhead.
func BenchmarkOldCVMStep(b *testing.B) {
	logger := zap.NewNop()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		vm, err := NewCVM(logger)
		if err != nil {
			b.Fatal(err)
		}

		ctx := ctxapi.NewRootContext()
		err = vm.StartString(ctx, `return 1`, "test")
		if err != nil {
			b.Fatal(err)
		}
		vm.Step()
		vm.Close()
	}
}

// BenchmarkOldCVMMemoryPerProcess measures memory per idle CVM.
func BenchmarkOldCVMMemoryPerProcess(b *testing.B) {
	logger := zap.NewNop()

	cvms := make([]*CoroutineVM, 0, b.N)

	runtime.GC()
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		vm, err := NewCVM(logger)
		if err != nil {
			b.Fatal(err)
		}

		ctx := ctxapi.NewRootContext()
		err = vm.StartString(ctx, `return 1`, "test")
		if err != nil {
			b.Fatal(err)
		}
		vm.Step()
		cvms = append(cvms, vm)
	}

	b.StopTimer()

	runtime.GC()
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	bytesPerProcess := float64(m2.Alloc-m1.Alloc) / float64(b.N)
	b.ReportMetric(bytesPerProcess, "bytes/process")

	for _, vm := range cvms {
		vm.Close()
	}
}
