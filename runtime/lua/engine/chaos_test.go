// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	scheduler "github.com/wippyai/runtime/system/scheduler/actor"
)

// Engine chaos: real Lua actors under the actor scheduler with random
// tick_budget and max_steps, driven by a seed. Environment:
//
//	WIPPY_LUA_CHAOS_SEED   first seed (default 1)
//	WIPPY_LUA_CHAOS_RUNS   number of consecutive seeds (default 3, 100 with WIPPY_CHAOS_LONG)
//	WIPPY_LUA_CHAOS_PROCS  actors per run (default 24, 150 with WIPPY_CHAOS_LONG)
func luaChaosEnvInt(name string, short, long int) int {
	def := short
	if os.Getenv("WIPPY_CHAOS_LONG") != "" {
		def = long
	}
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

var errLuaChaosStart = errors.New("lua chaos: start rejected")

type luaStage struct {
	body     string // Lua statements that leave the stage result in `result`
	expected string // value of `result`
	// entry budgets of the incarnation
	entry luaapi.ExecutionBudgets
	// delayed completion left outstanding by the incarnation before it upgrades
	staleMs int
	// upgrade requests an in-place upgrade after the body
	upgrade bool
}

type luaRec struct {
	rng         *rand.Rand
	cancel      context.CancelFunc
	spawn       attrs.Bag
	ready       chan struct{}
	result      *runtime.Result
	pid         pidapi.PID
	stages      []luaStage
	spawnBudget luaapi.ExecutionBudgets
	id          int
	resultMu    sync.Mutex
	starts      atomic.Int32
	upgrades    atomic.Int32
	submitted   atomic.Bool
	gaveUp      atomic.Bool
	killed      atomic.Bool
	running     atomic.Int32
	completes   atomic.Int32
	blocker     bool
	failing     bool
	wantsMsgs   bool
	wantsWake   bool
}

// limited reports whether any incarnation of the actor can be stopped by a
// max_steps limit.
func (r *luaRec) limited() bool {
	if r.spawnBudget.MaxSteps > 0 {
		return true
	}
	if r.spawnBudget.MaxStepsSet {
		return false
	}
	for _, s := range r.stages {
		if s.entry.MaxSteps > 0 {
			return true
		}
	}
	return false
}

// maxStepsOf is the effective max_steps of an incarnation.
func (r *luaRec) maxStepsOf(stage luaStage) uint64 {
	return stage.entry.Override(r.spawnBudget).MaxSteps
}

type luaChaos struct {
	sched      *scheduler.Scheduler
	appCtx     context.Context
	recs       []*luaRec
	incs       []*chaosLua
	violations []string
	seed       int64
	mu         sync.Mutex
	stopped    atomic.Bool
}

func (w *luaChaos) violatef(format string, args ...any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.violations) < 40 {
		w.violations = append(w.violations, fmt.Sprintf(format, args...))
	}
}

// chaosLua observes one incarnation's Lua process.
type chaosLua struct {
	*Process
	w       *luaChaos
	rec     *luaRec
	inc     int
	limit   uint64
	inits   atomic.Int32
	closed  atomic.Int32
	running atomic.Int32
	limited atomic.Bool
}

func (p *chaosLua) Init(ctx context.Context, method string, input payload.Payloads) error {
	p.inits.Add(1)
	return p.Process.Init(ctx, method, input)
}

func (p *chaosLua) Step(events []process.Event, out *process.StepOutput) error {
	if !p.running.CompareAndSwap(0, 1) {
		p.w.violatef("rec %d inc %d: concurrent steps of one incarnation", p.rec.id, p.inc)
	}
	defer p.running.Store(0)
	if p.rec.running.Add(1) != 1 {
		p.w.violatef("rec %d: steps of two incarnations overlap", p.rec.id)
	}
	defer p.rec.running.Add(-1)
	if p.rec.completes.Load() != 0 {
		p.w.violatef("rec %d inc %d: step after completion", p.rec.id, p.inc)
	}
	if p.closed.Load() != 0 {
		p.w.violatef("rec %d inc %d: step after close", p.rec.id, p.inc)
	}
	if p.limited.Load() {
		p.w.violatef("rec %d inc %d: step after the step limit failed it", p.rec.id, p.inc)
	}

	err := p.Process.Step(events, out)
	switch {
	case errors.Is(err, process.ErrStepLimitExceeded):
		p.limited.Store(true)
		if p.limit == 0 {
			p.w.violatef("rec %d inc %d: step limit error without max_steps", p.rec.id, p.inc)
		}
	case p.limit > 0 && p.StepsUsed() > p.limit:
		p.w.violatef("rec %d inc %d: %d steps used with max_steps %d (err %v)", p.rec.id, p.inc, p.StepsUsed(), p.limit, err)
	}
	return err
}

func (p *chaosLua) Close() {
	if p.closed.Add(1) != 1 {
		p.w.violatef("rec %d inc %d: closed more than once", p.rec.id, p.inc)
		return
	}
	if p.running.Load() != 0 {
		p.w.violatef("rec %d inc %d: closed during a step", p.rec.id, p.inc)
	}
	p.Process.Close()
}

type luaChaosFactory struct{ w *luaChaos }

func (f luaChaosFactory) Create(id registry.ID) (process.Process, *process.Meta, error) {
	n, err := strconv.Atoi(id.Name)
	if err != nil || n < 0 || n >= len(f.w.recs) {
		return nil, nil, fmt.Errorf("unknown chaos source %v", id)
	}
	rec := f.w.recs[n]
	inc := int(rec.upgrades.Add(1))
	if inc >= len(rec.stages) {
		f.w.violatef("rec %d: upgrade to incarnation %d but there are %d", rec.id, inc, len(rec.stages))
		return nil, nil, errLuaChaosStart
	}
	p, err := f.w.newProc(rec, inc)
	if err != nil {
		return nil, nil, err
	}
	return p, &process.Meta{}, nil
}

func (w *luaChaos) newProc(rec *luaRec, inc int) (*chaosLua, error) {
	stage := rec.stages[inc]
	src := w.script(rec, inc)
	upgradeReq := &UpgradeRequest{Source: registry.NewID("chaos", strconv.Itoa(rec.id))}
	proc, err := NewProcess(
		WithScript(src, fmt.Sprintf("chaos-%d-%d.lua", rec.id, inc)),
		WithProcessExecutionBudgets(stage.entry),
		WithModuleBinder(LoadCoreModules),
		WithModuleBinder(bindTestYield),
		WithModuleBinder(wrapBinder(func(l *lua.LState) { l.SetGlobal("upgrade_request", upgradeReq) })),
	)
	if err != nil {
		return nil, err
	}
	p := &chaosLua{Process: proc, w: w, rec: rec, inc: inc, limit: rec.maxStepsOf(stage)}
	w.mu.Lock()
	w.incs = append(w.incs, p)
	w.mu.Unlock()
	return p, nil
}

func (w *luaChaos) script(rec *luaRec, inc int) string {
	stage := rec.stages[inc]
	var b strings.Builder
	b.WriteString(stage.body)
	b.WriteString("\n")
	if stage.upgrade {
		if stage.staleMs > 0 {
			// A message wakes the actor while its spawned coroutine's yield is
			// still outstanding; the actor then upgrades.
			fmt.Fprintf(&b, "local wake = channel.new(1)\nsubscribe('wake', wake)\ncoroutine.spawn(function() test_yield(%d) end)\ncoroutine.yield()\nwake:receive()\n", stage.staleMs)
		}
		b.WriteString("coroutine.yield(upgrade_request)\n")
	}
	fmt.Fprintf(&b, "return \"gen%d:\" .. tostring(result)\n", inc)
	return b.String()
}

// luaChaosHandler completes a test_yield with its duration in milliseconds,
// after that duration: an actor that receives another value was resumed with
// a completion of a different yield.
func luaChaosHandler() dispatcher.Handler {
	return dispatcher.HandlerFunc(func(_ context.Context, cmd dispatcher.Command, tag uint64, r dispatcher.ResultReceiver) error {
		d := cmd.(testYieldCmd).Duration
		ms := d.Milliseconds()
		if d == 0 {
			r.CompleteYield(tag, ms, nil)
			return nil
		}
		time.AfterFunc(d, func() { r.CompleteYield(tag, ms, nil) })
		return nil
	})
}

func luaTriangle(n int) int { return n * (n + 1) / 2 }

// luaStageBody builds the body of a stage and its expected result.
func luaStageBody(rng *rand.Rand, rec *luaRec) (body, expected string) {
	switch rng.Intn(6) {
	case 0:
		n := 100 + rng.Intn(2000)
		return fmt.Sprintf("local s = 0\nfor i = 1, %d do s = s + i end\nlocal result = s", n), strconv.Itoa(luaTriangle(n))
	case 1:
		k, n, y := 2+rng.Intn(5), 50+rng.Intn(400), 1+rng.Intn(40)
		return fmt.Sprintf(`
local ch = channel.new(%[1]d)
for c = 1, %[1]d do
  coroutine.spawn(function()
    local s = 0
    for i = 1, %[2]d do
      s = s + i
      if i %% %[3]d == 0 then coroutine.yield() end
    end
    ch:send(s)
  end)
end
local total = 0
for c = 1, %[1]d do total = total + ch:receive() end
local result = total`, k, n, y), strconv.Itoa(k * luaTriangle(n))
	case 2:
		r := 1 + rng.Intn(4)
		var sb strings.Builder
		sum := 0
		sb.WriteString("local acc = 0\n")
		for i := 0; i < r; i++ {
			ms := rng.Intn(5)
			fmt.Fprintf(&sb, "do local r = test_yield(%d) if tonumber(r) ~= %d then error('wrong completion ' .. tostring(r) .. ' for %d') end acc = acc + tonumber(r) end\n", ms, ms, ms)
			sum += ms
		}
		sb.WriteString("local result = acc")
		return sb.String(), strconv.Itoa(sum)
	case 3:
		k := 2 + rng.Intn(4)
		var sum int
		for c := 1; c <= k; c++ {
			sum += c
		}
		return fmt.Sprintf(`
local ch = channel.new(%[1]d)
for c = 1, %[1]d do
  coroutine.spawn(function()
    local spin = 0
    for i = 1, 200 do spin = spin + i end
    local r = test_yield(c)
    if tonumber(r) ~= c then error('wrong completion ' .. tostring(r) .. ' for ' .. c) end
    ch:send(c)
  end)
end
local total = 0
for c = 1, %[1]d do total = total + ch:receive() end
local result = total`, k), strconv.Itoa(sum)
	case 4:
		m := 3 + rng.Intn(40)
		rec.wantsMsgs = true
		return fmt.Sprintf(`
local ch = channel.new(64)
subscribe("msg", ch)
local n = 0
while n < %d do
  ch:receive()
  n = n + 1
  for i = 1, 50 do n = n + 0 end
end
local result = n`, m), strconv.Itoa(m)
	default:
		n := 20 + rng.Intn(500)
		return fmt.Sprintf(`
local function fib(n) if n < 2 then return n end return fib(n - 1) + fib(n - 2) end
local result = fib(%d)`, 5+n%12), strconv.Itoa(fibRef(5 + n%12))
	}
}

func fibRef(n int) int {
	if n < 2 {
		return n
	}
	return fibRef(n-1) + fibRef(n-2)
}

func luaBudgetPick(rng *rand.Rand) luaapi.ExecutionBudgets {
	var b luaapi.ExecutionBudgets
	if rng.Intn(3) != 0 {
		ticks := []int64{0, -1, 1, 2, 7, 64, 1000, math.MaxInt64}
		b.TickBudget, b.TickBudgetSet = ticks[rng.Intn(len(ticks))], true
	}
	if rng.Intn(4) == 0 {
		steps := []uint64{0, 3, 12, 40, 1_000_000}
		b.MaxSteps, b.MaxStepsSet = steps[rng.Intn(len(steps))], true
	}
	return b
}

// optionValue renders an integer option in one of the representations spawn
// options arrive in.
func optionValue(rng *rand.Rand, v int64) any {
	switch rng.Intn(4) {
	case 0:
		return v
	case 1:
		return int(v)
	case 2:
		return float64(v)
	default:
		return json.Number(strconv.FormatInt(v, 10))
	}
}

func luaChaosPlan(rng *rand.Rand, rec *luaRec) {
	kind := rng.Intn(10)
	incarnations := 1
	if kind <= 3 {
		incarnations = 2 + rng.Intn(3)
	}
	for i := 0; i < incarnations; i++ {
		var st luaStage
		last := i == incarnations-1
		switch {
		case last && kind == 4:
			rec.failing = true
			st.body = "local s = 0 for i = 1, 300 do s = s + i end\nerror('chaos-boom')\nlocal result = s"
		case last && kind == 5:
			rec.blocker = true
			st.body = "local ch = channel.new(1)\nsubscribe('never', ch)\nch:receive()\nlocal result = 0"
		case last && kind == 6:
			rec.blocker = true
			st.body = "local n = 0\nwhile true do n = n + 1 end\nlocal result = n"
		default:
			st.body, st.expected = luaStageBody(rng, rec)
		}
		st.entry = luaBudgetPick(rng)
		if !last {
			st.upgrade = true
			if rng.Intn(3) == 0 {
				// The replacement waits on a yield longer than the stale one.
				st.staleMs = 8 + rng.Intn(8)
				rec.wantsWake = true
			}
		}
		rec.stages = append(rec.stages, st)
	}
	for i, st := range rec.stages {
		if st.staleMs > 0 && i+1 < len(rec.stages) {
			nxt := &rec.stages[i+1]
			nxt.body = "do local r = test_yield(40) if tonumber(r) ~= 40 then error('wrong completion ' .. tostring(r) .. ' for 40') end end\n" + nxt.body
		}
	}
	if rng.Intn(3) == 0 {
		var spawn luaapi.ExecutionBudgets
		bag := attrs.Bag{}
		if rng.Intn(2) == 0 {
			v := []int64{0, -1, 1, 5, 100, 5000}[rng.Intn(6)]
			bag[luaapi.ProcessOptionTickBudget] = optionValue(rng, v)
			spawn.TickBudget, spawn.TickBudgetSet = v, true
		}
		if rng.Intn(2) == 0 {
			v := []int64{0, 4, 15, 60, 2_000_000}[rng.Intn(5)]
			bag[luaapi.ProcessOptionMaxSteps] = optionValue(rng, v)
			spawn.MaxSteps, spawn.MaxStepsSet = uint64(v), true
		}
		rec.spawn, rec.spawnBudget = bag, spawn
	}
}

type luaChaosLifecycle struct{ w *luaChaos }

func (l luaChaosLifecycle) recFor(p pidapi.PID) *luaRec {
	n, err := strconv.Atoi(strings.TrimPrefix(p.UniqID, "luachaos-"))
	if err != nil || n < 0 || n >= len(l.w.recs) {
		return nil
	}
	return l.w.recs[n]
}

func (l luaChaosLifecycle) OnStart(_ context.Context, p pidapi.PID, _ process.Process) error {
	if rec := l.recFor(p); rec != nil && rec.starts.Add(1) != 1 {
		l.w.violatef("rec %d: started more than once", rec.id)
	}
	return nil
}

func (l luaChaosLifecycle) OnComplete(_ context.Context, p pidapi.PID, res *runtime.Result) {
	rec := l.recFor(p)
	if rec == nil {
		return
	}
	if rec.running.Load() != 0 {
		l.w.violatef("rec %d: completed during a step", rec.id)
	}
	if rec.completes.Add(1) != 1 {
		l.w.violatef("rec %d: completed more than once", rec.id)
		return
	}
	rec.resultMu.Lock()
	rec.result = res
	rec.resultMu.Unlock()
}

func (w *luaChaos) submit(rec *luaRec) {
	proc, err := w.newProc(rec, 0)
	if err != nil {
		w.violatef("rec %d: cannot build the process: %v", rec.id, err)
		rec.gaveUp.Store(true)
		close(rec.ready)
		return
	}
	frame, fc := ctxapi.OpenFrameContext(w.appCtx)
	if err := runtime.SetFrameID(frame, registry.NewID("chaos", strconv.Itoa(rec.id))); err != nil {
		w.violatef("rec %d: %v", rec.id, err)
	}
	if rec.spawn != nil {
		if err := fc.Set(runtime.FrameLifecycleOptionsKey, attrs.Attributes(rec.spawn)); err != nil {
			w.violatef("rec %d: %v", rec.id, err)
		}
	}
	ctx, cancel := context.WithCancel(frame)
	rec.cancel = cancel
	_, err = w.sched.Submit(ctx, rec.pid, proc, "", nil)
	if err != nil {
		cancel()
		proc.Close()
		if !errors.Is(err, process.ErrSchedulerStopping) {
			w.violatef("rec %d: unexpected Submit error %v", rec.id, err)
		}
		rec.gaveUp.Store(true)
		close(rec.ready)
		return
	}
	rec.submitted.Store(true)
	close(rec.ready)
}

func TestLuaChaos(t *testing.T) {
	seed := int64(luaChaosEnvInt("WIPPY_LUA_CHAOS_SEED", 1, 1))
	runs := luaChaosEnvInt("WIPPY_LUA_CHAOS_RUNS", 3, 100)
	for i := 0; i < runs; i++ {
		s := seed + int64(i)
		t.Run(fmt.Sprintf("seed=%d", s), func(t *testing.T) { runLuaChaos(t, s) })
	}
}

func runLuaChaos(t *testing.T, seed int64) {
	procs := luaChaosEnvInt("WIPPY_LUA_CHAOS_PROCS", 24, 150)
	rng := rand.New(rand.NewSource(seed))
	workers := 1 + rng.Intn(4)
	stopMode := rng.Intn(3)
	t.Logf("lua chaos seed=%d workers=%d procs=%d stopMode=%d (replay: WIPPY_LUA_CHAOS_SEED=%d WIPPY_LUA_CHAOS_RUNS=1)", seed, workers, procs, stopMode, seed)

	baseline := goruntime.NumGoroutine()
	w := &luaChaos{seed: seed}
	reg := newMockRegistry()
	reg.Register(testYieldCmdID, luaChaosHandler())
	w.sched = scheduler.NewScheduler(reg, scheduler.WithWorkers(workers), scheduler.WithLifecycle(luaChaosLifecycle{w: w}))

	appCtx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	process.WithFactory(appCtx, luaChaosFactory{w: w})
	w.appCtx = appCtx

	for i := 0; i < procs; i++ {
		rec := &luaRec{id: i, pid: pidapi.PID{Host: "test", UniqID: fmt.Sprintf("luachaos-%d", i)}, ready: make(chan struct{})}
		rec.rng = rand.New(rand.NewSource(seed*1000003 + int64(i)))
		luaChaosPlan(rec.rng, rec)
		w.recs = append(w.recs, rec)
	}

	w.sched.Start()

	var wg sync.WaitGroup
	for _, rec := range w.recs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(time.Duration(rec.rng.Intn(30_000)) * time.Microsecond)
			w.submit(rec)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-rec.ready
			if !rec.submitted.Load() {
				return
			}
			r := rec.rng
			deadline := time.Now().Add(20 * time.Second)
			killAt := time.Now().Add(time.Duration(r.Intn(40)) * time.Millisecond)
			kill := rec.blocker || r.Intn(8) == 0
			// Unconsumed messages stay queued in the actor, so the supply is bounded.
			sent := 0
			for rec.completes.Load() == 0 && time.Now().Before(deadline) && sent < 1500 {
				sent++
				if rec.wantsMsgs {
					pkg := relay.NewPackage(pidapi.PID{}, rec.pid, "msg", payload.NewPayload(lua.LString("x"), payload.Lua))
					if err := w.sched.Send(pkg); err != nil && !errors.Is(err, process.ErrProcessNotFound) && !errors.Is(err, process.ErrProcessClosed) {
						w.violatef("rec %d: unexpected Send error %v", rec.id, err)
					}
				}
				if rec.wantsWake {
					pkg := relay.NewPackage(pidapi.PID{}, rec.pid, "wake", payload.NewPayload(lua.LString("x"), payload.Lua))
					_ = w.sched.Send(pkg)
				}
				if kill && time.Now().After(killAt) {
					rec.killed.Store(true)
					if r.Intn(2) == 0 {
						rec.cancel()
					} else if err := w.sched.Terminate(rec.pid); err != nil && !errors.Is(err, process.ErrProcessNotFound) {
						w.violatef("rec %d: unexpected Terminate error %v", rec.id, err)
					}
					return
				}
				time.Sleep(time.Duration(300+r.Intn(700)) * time.Microsecond)
			}
		}()
	}

	stopTimeout := 20 * time.Second
	if stopMode == 2 {
		stopTimeout = time.Duration(20+rng.Intn(80)) * time.Millisecond
	}
	stopDone := make(chan struct{})
	startStop := func() {
		w.stopped.Store(true)
		ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
		w.sched.Stop(ctx)
		cancel()
		close(stopDone)
	}
	if stopMode != 0 {
		time.AfterFunc(time.Duration(rng.Intn(80))*time.Millisecond, func() { go startStop() })
	}

	wg.Wait()
	if stopMode == 0 {
		w.awaitSettled(t, 60*time.Second)
		go startStop()
	}
	select {
	case <-stopDone:
	case <-time.After(60 * time.Second):
		t.Fatalf("seed %d: Stop did not return", seed)
	}
	w.awaitSettled(t, 10*time.Second)
	w.verify(t, seed)

	deadline := time.Now().Add(10 * time.Second)
	for goruntime.NumGoroutine() > baseline+2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := goruntime.NumGoroutine(); n > baseline+2 {
		buf := make([]byte, 1<<20)
		t.Errorf("seed %d: %d goroutines remain, baseline %d\n%s", seed, n, baseline, buf[:goruntime.Stack(buf, true)])
	}
}

func (w *luaChaos) awaitSettled(t *testing.T, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		var stranded []string
		for _, rec := range w.recs {
			select {
			case <-rec.ready:
			default:
				stranded = append(stranded, fmt.Sprintf("rec %d never submitted", rec.id))
				continue
			}
			if rec.submitted.Load() && rec.completes.Load() == 0 {
				stranded = append(stranded, fmt.Sprintf("rec %d (blocker=%v stages=%d) stranded", rec.id, rec.blocker, len(rec.stages)))
			}
		}
		if len(stranded) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("seed %d: %d actors stranded: %v\nprocesses: %+v", w.seed, len(stranded), stranded[:min(len(stranded), 10)], w.sched.ListProcesses())
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (w *luaChaos) verify(t *testing.T, seed int64) {
	t.Helper()
	outcomes := map[string]int{}
	for _, rec := range w.recs {
		if rec.gaveUp.Load() {
			outcomes["rejected"]++
			if rec.completes.Load() != 0 {
				w.violatef("rec %d: completed although admission failed", rec.id)
			}
			continue
		}
		if n := rec.completes.Load(); n != 1 {
			w.violatef("rec %d: completed %d times", rec.id, n)
			continue
		}
		rec.resultMu.Lock()
		res := rec.result
		rec.resultMu.Unlock()
		outcomes[w.checkOutcome(rec, res)]++
	}
	t.Logf("seed %d outcomes %v", seed, outcomes)

	w.mu.Lock()
	incs := append([]*chaosLua(nil), w.incs...)
	w.mu.Unlock()
	for _, p := range incs {
		if p.inits.Load() > 0 && p.closed.Load() != 1 {
			w.violatef("rec %d inc %d: closed %d times, want once", p.rec.id, p.inc, p.closed.Load())
		}
	}

	stats := w.sched.Stats()
	if stats["processes"] != 0 {
		w.violatef("%d processes still registered after stop", stats["processes"])
	}
	if stats["global_queue"] != 0 {
		w.violatef("global queue holds %d entries after stop", stats["global_queue"])
	}
	if len(w.violations) > 0 {
		t.Errorf("seed %d: %d invariant violations (replay: WIPPY_LUA_CHAOS_SEED=%d WIPPY_LUA_CHAOS_RUNS=1):\n  %s",
			seed, len(w.violations), seed, strings.Join(w.violations, "\n  "))
	}
}

// checkOutcome verifies a finished actor and names its outcome class.
func (w *luaChaos) checkOutcome(rec *luaRec, res *runtime.Result) string {
	if res == nil {
		w.violatef("rec %d: no result", rec.id)
		return "none"
	}
	killed := rec.killed.Load() || w.stopped.Load()
	last := rec.stages[len(rec.stages)-1]
	if res.Error == nil {
		var got string
		if res.Value != nil {
			if v, ok := res.Value.Data().(lua.LValue); ok {
				got = lua.LVAsString(v)
			}
		}
		want := fmt.Sprintf("gen%d:%s", len(rec.stages)-1, last.expected)
		if rec.failing || rec.blocker {
			w.violatef("rec %d: finished with %q although its final stage cannot finish", rec.id, got)
		} else if got != want {
			w.violatef("rec %d: result %q, want %q", rec.id, got, want)
		}
		return "done"
	}
	switch {
	case errors.Is(res.Error, process.ErrStepLimitExceeded):
		if !rec.limited() {
			w.violatef("rec %d: step limit error without max_steps", rec.id)
		}
		return "limit"
	case rec.failing && strings.Contains(res.Error.Error(), "chaos-boom"):
		return "fail"
	case killed:
		return "killed"
	}
	w.violatef("rec %d: unexpected error %v", rec.id, res.Error)
	return "unexpected"
}
