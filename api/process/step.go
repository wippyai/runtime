package process

import "sync"

// StepStatus indicates the process state after Step() returns.
const (
	StepContinue StepStatus = iota
	StepIdle
	StepDone
)

// MaxYields is the maximum yields per step that fit in the fixed buffer.
const MaxYields = 2

// StepStatus indicates the process state after Step() returns.
type StepStatus int

// StepResult is returned by Process.Step() containing status and yields.
type StepResult struct {
	Status     StepStatus
	Result     Payload
	yieldCount int
	yieldsBuf  [MaxYields]Command
	yields     []Command
}

// YieldResults carries results from handler execution back to the process.
type YieldResults struct {
	Data  any
	Error error
}

// GetYields returns the yielded commands.
func (r *StepResult) GetYields() []Command {
	if r.yields != nil {
		return r.yields
	}
	return r.yieldsBuf[:r.yieldCount]
}

// AddYield appends a command to the result.
func (r *StepResult) AddYield(cmd Command) {
	if r.yieldCount < MaxYields {
		r.yieldsBuf[r.yieldCount] = cmd
		r.yieldCount++
	} else {
		if r.yields == nil {
			r.yields = make([]Command, MaxYields, MaxYields*2)
			copy(r.yields, r.yieldsBuf[:])
		}
		r.yields = append(r.yields, cmd)
	}
}

// YieldCount returns the number of yielded commands.
func (r *StepResult) YieldCount() int {
	if r.yields != nil {
		return len(r.yields)
	}
	return r.yieldCount
}

// Reset clears the result for reuse.
func (r *StepResult) Reset() {
	r.Status = StepContinue
	r.Result = nil
	for i := 0; i < r.yieldCount; i++ {
		r.yieldsBuf[i] = nil
	}
	r.yieldCount = 0
	r.yields = nil
}

var yieldResultsPool = sync.Pool{
	New: func() any { return &YieldResults{} },
}

// AcquireYieldResults gets a YieldResults from pool.
func AcquireYieldResults() *YieldResults {
	return yieldResultsPool.Get().(*YieldResults)
}

// ReleaseYieldResults returns a YieldResults to pool.
func ReleaseYieldResults(yr *YieldResults) {
	yr.Data = nil
	yr.Error = nil
	yieldResultsPool.Put(yr)
}
