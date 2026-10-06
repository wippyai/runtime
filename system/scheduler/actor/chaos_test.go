// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/payload"
	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	apiruntime "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
	sysprocess "github.com/wippyai/runtime/system/process"
	"github.com/wippyai/runtime/system/scheduler"
)

// Chaos configuration. Defaults keep a normal run fast; the environment
// selects longer runs and replays a failing seed:
//
//	WIPPY_CHAOS_SEED   first seed (default 1)
//	WIPPY_CHAOS_RUNS   number of consecutive seeds (default 4, 200 with WIPPY_CHAOS_LONG)
//	WIPPY_CHAOS_PROCS  processes per run (default 120, 600 with WIPPY_CHAOS_LONG)
//	WIPPY_CHAOS_LONG   any non-empty value selects the long defaults
func chaosEnvInt(name string, short, long int) int {
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

const cmdChaosAsync dispatcher.CommandID = 100

// cmdChaosUnregistered has no handler, so a yield of it completes with an
// unknown-command error.
const cmdChaosUnregistered dispatcher.CommandID = 101

var (
	errChaosFail    = errors.New("chaos: planned failure")
	errChaosHandler = errors.New("chaos: handler failure")
	errChaosCancel  = errors.New("chaos: cancelled")
	errChaosStart   = errors.New("chaos: start rejected")
)

type chaosAsyncMode uint8

const (
	asyncLater      chaosAsyncMode = iota // completes from another goroutine after a delay
	asyncInline                           // completes inside Handle
	asyncHandleErr                        // Handle returns an error
	asyncLaterError                       // completes with an error after a delay
	asyncUnknown                          // no handler
)

type chaosAsyncCmd struct {
	token chaosToken
	delay time.Duration
	mode  chaosAsyncMode
}

func (c chaosAsyncCmd) CmdID() dispatcher.CommandID {
	if c.mode == asyncUnknown {
		return cmdChaosUnregistered
	}
	return cmdChaosAsync
}

// chaosToken identifies the yield a completion answers. A completion that
// reaches an incarnation other than the one that issued it is a defect.
type chaosToken struct {
	rec int
	inc int
	tag uint64
}

type chaosOpKind uint8

const (
	opSpin         chaosOpKind = iota // n steps, each reporting Preempt or Continue
	opAsync                           // yield and wait for the completion
	opYieldPreempt                    // yield and report Preempt in the same step, then wait
	opWaitMsg                         // wait until n messages arrived in total
	opStaleUpgrade                    // yield with a long delay, preempt, upgrade without waiting
	opUpgrade
	opFail
	opFinish
	opNever // wait for termination
)

type chaosOp struct {
	delay time.Duration
	n     int
	mode  chaosAsyncMode
	kind  chaosOpKind
}

// chaosRec is the shared state of one process across its incarnations.
type chaosRec struct {
	result       *apiruntime.Result
	rng          *rand.Rand
	cancelParent context.CancelFunc
	ready        chan struct{}
	pid          pidapi.PID
	plans        [][]chaosOp
	failUpgrade  int
	need         int64
	id           int
	stepsRun     atomic.Uint64
	msgs         atomic.Int64
	maxSteps     uint64
	resultMu     sync.Mutex
	killed       atomic.Bool
	upgrades     atomic.Int32
	running      atomic.Int32
	submitted    atomic.Bool
	gaveUp       atomic.Bool
	starts       atomic.Int32
	completes    atomic.Int32
	failStart    bool
	deaf         bool
	blocker      bool
	pooled       bool
	failUpInit   bool
}

func (r *chaosRec) expectsLimit() bool { return r.maxSteps > 0 }

type chaosWorld struct {
	sched      *Scheduler
	appCtx     context.Context
	t          *testing.T
	recs       []*chaosRec
	incs       []*chaosProc
	violations []string
	seed       int64
	mu         sync.Mutex
	stopped    atomic.Bool
}

func (w *chaosWorld) violatef(format string, args ...any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.violations) < 40 {
		w.violations = append(w.violations, fmt.Sprintf(format, args...))
	}
}

func (w *chaosWorld) track(p *chaosProc) {
	w.mu.Lock()
	w.incs = append(w.incs, p)
	w.mu.Unlock()
}

// chaosProc is one incarnation of a chaos actor.
type chaosProc struct {
	w           *chaosWorld
	rec         *chaosRec
	rng         *rand.Rand
	outstanding map[uint64]struct{}
	plan        []chaosOp
	steps       uint64
	nextTag     uint64
	waitTag     uint64
	pc          int
	ph          int
	inc         int
	closed      atomic.Int32
	running     atomic.Int32
	inits       atomic.Int32
	preempt     bool
	limited     bool
}

var (
	_ process.Process       = (*chaosProc)(nil)
	_ process.Preemptible   = (*chaosProc)(nil)
	_ process.StepAccounted = (*chaosProc)(nil)
)

func (p *chaosProc) EnablePreemption()           { p.preempt = true }
func (p *chaosProc) StepsUsed() uint64           { return p.steps }
func (p *chaosProc) ResumeStepCount(used uint64) { p.steps = used }

func (p *chaosProc) Init(context.Context, string, payload.Payloads) error {
	if p.inits.Add(1) != 1 {
		p.w.violatef("rec %d inc %d: Init called more than once", p.rec.id, p.inc)
	}
	if p.rec.failUpInit && p.inc > 0 {
		return errChaosStart
	}
	return nil
}

func (p *chaosProc) Close() {
	if p.closed.Add(1) != 1 {
		p.w.violatef("rec %d inc %d: closed more than once", p.rec.id, p.inc)
	}
	if p.running.Load() != 0 {
		p.w.violatef("rec %d inc %d: closed during a step", p.rec.id, p.inc)
	}
}

func isCancelEvent(data any) bool {
	pkg, ok := data.(*relay.Package)
	if !ok || pkg == nil || len(pkg.Messages) != 1 || pkg.Messages[0].Topic != topology.TopicEvents || len(pkg.Messages[0].Payloads) != 1 {
		return false
	}
	_, ok = pkg.Messages[0].Payloads[0].Data().(*topology.CancelEvent)
	return ok
}

func (p *chaosProc) Step(events []process.Event, out *process.StepOutput) error {
	rec := p.rec
	if !p.running.CompareAndSwap(0, 1) {
		p.w.violatef("rec %d inc %d: concurrent steps of one incarnation", rec.id, p.inc)
	}
	defer p.running.Store(0)
	if rec.running.Add(1) != 1 {
		p.w.violatef("rec %d: steps of two incarnations overlap", rec.id)
	}
	defer rec.running.Add(-1)

	if rec.completes.Load() != 0 {
		p.w.violatef("rec %d inc %d: step after completion", rec.id, p.inc)
	}
	if p.closed.Load() != 0 {
		p.w.violatef("rec %d inc %d: step after close", rec.id, p.inc)
	}
	if p.limited {
		p.w.violatef("rec %d inc %d: step after the step limit failed it", rec.id, p.inc)
	}

	p.steps++
	if rec.maxSteps > 0 && p.steps > rec.maxSteps {
		p.limited = true
		return process.ErrStepLimitExceeded
	}
	total := rec.stepsRun.Add(1)
	if rec.maxSteps > 0 && total > rec.maxSteps {
		p.w.violatef("rec %d: %d steps ran with max_steps %d", rec.id, total, rec.maxSteps)
	}

	cancelled := false
	for _, e := range events {
		switch e.Type {
		case process.EventYieldComplete:
			p.absorb(e)
		case process.EventMessage:
			if isCancelEvent(e.Data) {
				cancelled = true
			} else if e.Data != nil {
				rec.msgs.Add(1)
			}
		}
	}
	if cancelled && !rec.deaf {
		return errChaosCancel
	}
	return p.advance(out)
}

func (p *chaosProc) absorb(e process.Event) {
	rec := p.rec
	if _, ok := p.outstanding[e.Tag]; !ok {
		p.w.violatef("rec %d inc %d: completion for tag %d that is not outstanding (data %#v)", rec.id, p.inc, e.Tag, e.Data)
		return
	}
	delete(p.outstanding, e.Tag)
	if e.Error != nil {
		return
	}
	tok, ok := e.Data.(chaosToken)
	if !ok || tok.rec != rec.id || tok.inc != p.inc || tok.tag != e.Tag {
		p.w.violatef("rec %d inc %d: completion %#v delivered to the wrong yield (tag %d)", rec.id, p.inc, e.Data, e.Tag)
	}
}

func (p *chaosProc) yield(out *process.StepOutput, op chaosOp) uint64 {
	p.nextTag++
	tag := p.nextTag
	p.outstanding[tag] = struct{}{}
	out.Yield(chaosAsyncCmd{token: chaosToken{rec: p.rec.id, inc: p.inc, tag: tag}, delay: op.delay, mode: op.mode}, tag)
	return tag
}

func (p *chaosProc) next() { p.pc++; p.ph = 0 }

func (p *chaosProc) advance(out *process.StepOutput) error {
	for {
		if p.pc >= len(p.plan) {
			out.Done(payload.New(chaosToken{rec: p.rec.id, inc: p.inc}))
			return nil
		}
		op := p.plan[p.pc]
		switch op.kind {
		case opSpin:
			if p.ph >= op.n {
				p.next()
				continue
			}
			p.ph++
			if p.rng.Intn(3) == 0 {
				out.Continue()
			} else {
				out.Preempt()
			}
			return nil
		case opAsync:
			if p.ph == 0 {
				p.waitTag = p.yield(out, op)
				p.ph = 1
				out.WaitForYields()
				return nil
			}
			if _, pending := p.outstanding[p.waitTag]; pending {
				out.WaitForYields()
				return nil
			}
			p.next()
		case opYieldPreempt:
			if p.ph == 0 {
				p.waitTag = p.yield(out, op)
				p.ph = 1
				out.Preempt()
				return nil
			}
			if _, pending := p.outstanding[p.waitTag]; pending {
				out.WaitForYields()
				return nil
			}
			p.next()
		case opWaitMsg:
			if p.rec.msgs.Load() >= int64(op.n) {
				p.next()
				continue
			}
			out.Idle()
			return nil
		case opStaleUpgrade:
			if p.ph == 0 {
				p.yield(out, op)
				p.ph = 1
				out.Preempt()
				return nil
			}
			out.SetUpgrade(&process.UpgradeRequest{Source: registry.NewID("chaos", strconv.Itoa(p.rec.id))})
			return nil
		case opUpgrade:
			out.SetUpgrade(&process.UpgradeRequest{Source: registry.NewID("chaos", strconv.Itoa(p.rec.id))})
			return nil
		case opFail:
			return errChaosFail
		case opFinish:
			out.Done(payload.New(chaosToken{rec: p.rec.id, inc: p.inc}))
			return nil
		case opNever:
			out.Idle()
			return nil
		}
	}
}

type chaosFactory struct{ w *chaosWorld }

func (f chaosFactory) Create(id registry.ID) (process.Process, *process.Meta, error) {
	n, err := strconv.Atoi(id.Name)
	if err != nil || n < 0 || n >= len(f.w.recs) {
		return nil, nil, fmt.Errorf("unknown chaos source %v", id)
	}
	rec := f.w.recs[n]
	inc := int(rec.upgrades.Add(1))
	if rec.failUpgrade == inc {
		return nil, nil, errChaosStart
	}
	if inc >= len(rec.plans) {
		f.w.violatef("rec %d: upgrade to incarnation %d but the plan has %d", rec.id, inc, len(rec.plans))
		return nil, nil, errChaosStart
	}
	return f.w.newProc(rec, inc), &process.Meta{Method: "main"}, nil
}

func (w *chaosWorld) newProc(rec *chaosRec, inc int) *chaosProc {
	p := &chaosProc{
		w:           w,
		rec:         rec,
		inc:         inc,
		plan:        rec.plans[inc],
		rng:         rand.New(rand.NewSource(int64(rec.id)*7919 + int64(inc) + w.seed)),
		outstanding: make(map[uint64]struct{}, 4),
	}
	w.track(p)
	return p
}

func chaosHandler() dispatcher.Handler {
	return dispatcher.HandlerFunc(func(_ context.Context, cmd dispatcher.Command, tag uint64, r dispatcher.ResultReceiver) error {
		c := cmd.(chaosAsyncCmd)
		switch c.mode {
		case asyncInline:
			r.CompleteYield(tag, c.token, nil)
		case asyncHandleErr:
			return errChaosHandler
		case asyncLaterError:
			time.AfterFunc(c.delay, func() { r.CompleteYield(tag, nil, errChaosHandler) })
		default:
			time.AfterFunc(c.delay, func() { r.CompleteYield(tag, c.token, nil) })
		}
		return nil
	})
}

type chaosLifecycle struct{ w *chaosWorld }

func (l chaosLifecycle) recFor(p pidapi.PID) *chaosRec {
	n, err := strconv.Atoi(strings.TrimPrefix(p.UniqID, "chaos-"))
	if err != nil || n < 0 || n >= len(l.w.recs) {
		return nil
	}
	return l.w.recs[n]
}

func (l chaosLifecycle) OnStart(_ context.Context, p pidapi.PID, _ process.Process) error {
	rec := l.recFor(p)
	if rec == nil {
		return nil
	}
	if rec.starts.Add(1) != 1 {
		l.w.violatef("rec %d: started more than once", rec.id)
	}
	if rec.failStart {
		return errChaosStart
	}
	return nil
}

func (l chaosLifecycle) OnComplete(_ context.Context, p pidapi.PID, res *apiruntime.Result) {
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

func chaosPlanFor(rng *rand.Rand, rec *chaosRec) {
	randDelay := func() time.Duration {
		switch rng.Intn(10) {
		case 0:
			return 0
		case 1:
			return time.Duration(5+rng.Intn(15)) * time.Millisecond
		default:
			return time.Duration(rng.Intn(1500)) * time.Microsecond
		}
	}
	middle := func() []chaosOp {
		var ops []chaosOp
		for i := rng.Intn(5); i > 0; i-- {
			switch rng.Intn(6) {
			case 0:
				ops = append(ops, chaosOp{kind: opSpin, n: 1 + rng.Intn(12)})
			case 1, 2:
				ops = append(ops, chaosOp{kind: opAsync, delay: randDelay(), mode: chaosAsyncMode(rng.Intn(5))})
			case 3:
				ops = append(ops, chaosOp{kind: opYieldPreempt, delay: randDelay(), mode: asyncLater})
			case 4:
				ops = append(ops, chaosOp{kind: opAsync, delay: randDelay(), mode: asyncLaterError})
			default:
				ops = append(ops, chaosOp{kind: opSpin, n: 1 + rng.Intn(3)})
			}
		}
		return ops
	}

	kind := rng.Intn(10)
	incarnations := 1
	if kind <= 3 {
		incarnations = 2 + rng.Intn(3)
	}
	stale := false
	for i := 0; i < incarnations; i++ {
		plan := middle()
		if stale {
			// The replacement waits on a yield with the same tag the replaced
			// code left outstanding.
			plan = append([]chaosOp{{kind: opAsync, delay: 12 * time.Millisecond, mode: asyncLater}}, plan...)
			stale = false
		}
		if i < incarnations-1 {
			if rng.Intn(3) == 0 {
				plan = append(plan, chaosOp{kind: opStaleUpgrade, delay: time.Duration(1+rng.Intn(4)) * time.Millisecond})
				stale = true
			} else {
				plan = append(plan, chaosOp{kind: opUpgrade})
			}
		} else {
			switch kind {
			case 4:
				plan = append(plan, chaosOp{kind: opFail})
			case 5:
				rec.blocker = true
				plan = append(plan, chaosOp{kind: opNever})
			case 6:
				rec.need = int64(50 + rng.Intn(400))
				plan = append(plan, chaosOp{kind: opWaitMsg, n: int(rec.need)}, chaosOp{kind: opFinish})
			default:
				plan = append(plan, chaosOp{kind: opFinish})
			}
		}
		if i == 0 && kind != 6 && rng.Intn(3) == 0 {
			n := 1 + rng.Intn(20)
			plan = append([]chaosOp{{kind: opWaitMsg, n: n}}, plan...)
			if int64(n) > rec.need {
				rec.need = int64(n)
			}
		}
		rec.plans = append(rec.plans, plan)
	}
	if rng.Intn(5) == 0 {
		rec.maxSteps = uint64(3 + rng.Intn(60))
	}
	rec.deaf = rng.Intn(8) == 0
	if incarnations > 1 {
		switch rng.Intn(10) {
		case 0:
			rec.failUpgrade = 1 + rng.Intn(incarnations-1)
		case 1:
			rec.failUpInit = true
		}
	}
	rec.failStart = rng.Intn(25) == 0
}

func (w *chaosWorld) submit(rec *chaosRec) {
	proc := w.newProc(rec, 0)
	frame, fc := ctxapi.OpenFrameContext(w.appCtx)
	fc.Seal()
	ctx, cancel := context.WithCancel(frame)
	rec.cancelParent = cancel
	_, err := w.sched.Submit(ctx, rec.pid, proc, "main", nil)
	if err != nil {
		cancel()
		proc.Close()
		switch {
		case errors.Is(err, process.ErrSchedulerStopping), errors.Is(err, process.ErrMaxProcessesExceeded), errors.Is(err, errChaosStart):
		default:
			w.violatef("rec %d: unexpected Submit error %v", rec.id, err)
		}
		if rec.completes.Load() != 0 {
			w.violatef("rec %d: completed although Submit failed", rec.id)
		}
		rec.gaveUp.Store(true)
		close(rec.ready)
		return
	}
	rec.submitted.Store(true)
	close(rec.ready)
}

// submitPooled runs a process the way a pooled executor does: the owner
// creates the processor, schedules it, reads its result and releases it.
func (w *chaosWorld) submitPooled(rec *chaosRec) {
	proc := w.newProc(rec, 0)
	frame, fc := ctxapi.OpenFrameContext(w.appCtx)
	fc.Seal()
	ctx, cancel := context.WithCancel(frame)
	defer cancel()
	pr, err := w.sched.CreateProcessor(ctx, rec.pid, proc)
	if err != nil {
		proc.Close()
		if !errors.Is(err, process.ErrSchedulerStopping) && !errors.Is(err, process.ErrMaxProcessesExceeded) {
			w.violatef("rec %d: unexpected CreateProcessor error %v", rec.id, err)
		}
		rec.gaveUp.Store(true)
		close(rec.ready)
		return
	}
	rec.submitted.Store(true)
	rec.cancelParent = cancel
	close(rec.ready)
	results := pr.resultCh
	if err := proc.Init(ctx, "main", nil); err != nil {
		w.sched.ReleaseProcessor(pr)
		return
	}
	w.sched.global.Push(pr)
	w.sched.wakeAny()
	select {
	case <-results:
	case <-time.After(30 * time.Second):
		w.violatef("rec %d: pooled execution did not complete", rec.id)
	}
	w.sched.ReleaseProcessor(pr)
}

func (w *chaosWorld) allowedSendError(err error) bool {
	return err == nil || errors.Is(err, process.ErrProcessNotFound) || errors.Is(err, process.ErrProcessClosed)
}

func (w *chaosWorld) send(rec *chaosRec) {
	err := w.sched.Send(&relay.Package{Target: rec.pid, Messages: []*relay.Message{{Topic: "chaos"}}})
	if !w.allowedSendError(err) {
		w.violatef("rec %d: unexpected Send error %v", rec.id, err)
	}
}

func chaosGoroutineSettle(baseline int, d time.Duration) (int, bool) {
	deadline := time.Now().Add(d)
	for {
		n := goruntime.NumGoroutine()
		if n <= baseline+2 {
			return n, true
		}
		if time.Now().After(deadline) {
			return n, false
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSchedulerChaos(t *testing.T) {
	seed := int64(chaosEnvInt("WIPPY_CHAOS_SEED", 1, 1))
	runs := chaosEnvInt("WIPPY_CHAOS_RUNS", 4, 200)
	for i := 0; i < runs; i++ {
		s := seed + int64(i)
		t.Run(fmt.Sprintf("seed=%d", s), func(t *testing.T) { runSchedulerChaos(t, s) })
	}
}

func runSchedulerChaos(t *testing.T, seed int64) {
	procs := chaosEnvInt("WIPPY_CHAOS_PROCS", 120, 600)
	rng := rand.New(rand.NewSource(seed))
	workers := 1 + rng.Intn(6)
	maxProcs := int64(0)
	if rng.Intn(4) == 0 {
		maxProcs = int64(procs / 2)
	}
	stopMode := rng.Intn(3) // 0: stop after the run, 1: stop at a random time, 2: stop at a random time with a short deadline
	t.Logf("chaos seed=%d workers=%d procs=%d maxProcs=%d stopMode=%d (replay: WIPPY_CHAOS_SEED=%d WIPPY_CHAOS_RUNS=1)", seed, workers, procs, maxProcs, stopMode, seed)

	baseline := goruntime.NumGoroutine()

	w := &chaosWorld{t: t, seed: seed}
	reg := scheduler.NewRegistry()
	reg.Register(cmdChaosAsync, chaosHandler())
	opts := []Option{WithWorkers(workers), WithLifecycle(chaosLifecycle{w: w}), WithLocalQueueSize(8 << rng.Intn(5))}
	if maxProcs > 0 {
		opts = append(opts, WithMaxProcesses(maxProcs))
	}
	w.sched = NewScheduler(reg, opts...)

	appCtx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
	process.WithFactory(appCtx, chaosFactory{w: w})
	w.appCtx = appCtx

	for i := 0; i < procs; i++ {
		rec := &chaosRec{id: i, pid: pidapi.PID{UniqID: fmt.Sprintf("chaos-%d", i)}, ready: make(chan struct{})}
		rec.rng = rand.New(rand.NewSource(seed*1000003 + int64(i)))
		chaosPlanFor(rec.rng, rec)
		rec.pooled = rec.rng.Intn(12) == 0 && len(rec.plans) == 1
		if rec.pooled {
			rec.failStart = false
			rec.failUpgrade = 0
		}
		w.recs = append(w.recs, rec)
	}

	w.sched.Start()

	var wg sync.WaitGroup
	for _, rec := range w.recs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(time.Duration(rec.rng.Intn(40_000)) * time.Microsecond)
			if rec.pooled {
				w.submitPooled(rec)
			} else {
				w.submit(rec)
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-rec.ready
			if !rec.submitted.Load() {
				return
			}
			r := rec.rng
			for sent := int64(0); sent < rec.need+int64(r.Intn(8)); sent++ {
				w.send(rec)
				if r.Intn(16) == 0 {
					time.Sleep(time.Duration(r.Intn(500)) * time.Microsecond)
				}
			}
			w.send(&chaosRec{pid: pidapi.PID{UniqID: "chaos-missing"}, id: -1})
			time.Sleep(time.Duration(r.Intn(15)) * time.Millisecond)
			if rec.blocker || r.Intn(7) == 0 {
				rec.killed.Store(true)
				if r.Intn(2) == 0 {
					rec.cancelParent()
				} else if err := w.sched.Terminate(rec.pid); err != nil && !errors.Is(err, process.ErrProcessNotFound) {
					w.violatef("rec %d: unexpected Terminate error %v", rec.id, err)
				}
			}
		}()
	}

	if rng.Intn(2) == 0 {
		resizeSeed := rng.Int63()
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := rand.New(rand.NewSource(resizeSeed))
			for i := 0; i < 20; i++ {
				time.Sleep(time.Duration(r.Intn(4000)) * time.Microsecond)
				if err := w.sched.ResizeWorkers(1 + r.Intn(6)); err != nil && !errors.Is(err, process.ErrSchedulerStopping) {
					w.violatef("unexpected ResizeWorkers error %v", err)
				}
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
		started := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
		w.sched.Stop(ctx)
		cancel()
		if stopMode != 2 && time.Since(started) > stopTimeout-time.Second {
			w.violatef("Stop took %v, close to its %v deadline", time.Since(started), stopTimeout)
		}
		close(stopDone)
	}
	if stopMode != 0 {
		time.AfterFunc(time.Duration(rng.Intn(60))*time.Millisecond, func() { go startStop() })
	}

	wg.Wait()
	if stopMode == 0 {
		w.awaitSettled(t, 30*time.Second)
		go startStop()
	}
	select {
	case <-stopDone:
	case <-time.After(60 * time.Second):
		t.Fatalf("seed %d: Stop did not return (goroutines dumped below)\n%s", seed, chaosStacks())
	}
	w.awaitSettled(t, 10*time.Second)

	w.verify(t, seed)

	if n, ok := chaosGoroutineSettle(baseline, 10*time.Second); !ok {
		t.Errorf("seed %d: %d goroutines remain, baseline %d\n%s", seed, n, baseline, chaosStacks())
	}
}

func chaosStacks() string {
	buf := make([]byte, 1<<20)
	return string(buf[:goruntime.Stack(buf, true)])
}

// awaitSettled waits until every record reached a terminal state.
func (w *chaosWorld) awaitSettled(t *testing.T, d time.Duration) {
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
				stranded = append(stranded, fmt.Sprintf("rec %d (pooled=%v blocker=%v deaf=%v plans=%d) stranded", rec.id, rec.pooled, rec.blocker, rec.deaf, len(rec.plans)))
			}
		}
		if len(stranded) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("seed %d: %d processes stranded: %v\nprocesses: %+v", w.seed, len(stranded), stranded[:min(len(stranded), 10)], w.sched.ListProcesses())
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (w *chaosWorld) verify(t *testing.T, seed int64) {
	t.Helper()
	outcomes := map[string]int{}
	upgrades, rejected := 0, 0
	for _, rec := range w.recs {
		starts, completes := rec.starts.Load(), rec.completes.Load()
		if rec.gaveUp.Load() {
			rejected++
			if completes != 0 {
				w.violatef("rec %d: completed although admission failed", rec.id)
			}
			continue
		}
		if completes != 1 {
			w.violatef("rec %d: completed %d times (starts %d)", rec.id, completes, starts)
			continue
		}
		rec.resultMu.Lock()
		res := rec.result
		rec.resultMu.Unlock()
		w.checkOutcome(rec, res)
		outcomes[outcomeName(res)]++
		upgrades += int(rec.upgrades.Load())
	}
	t.Logf("seed %d outcomes %v admitted-failures=%d upgrades=%d", seed, outcomes, rejected, upgrades)

	w.mu.Lock()
	incs := append([]*chaosProc(nil), w.incs...)
	w.mu.Unlock()
	for _, p := range incs {
		if p.inits.Load() > 0 && p.closed.Load() != 1 {
			w.violatef("rec %d inc %d: closed %d times, want once", p.rec.id, p.inc, p.closed.Load())
		}
	}

	s := w.sched
	if n := s.parked.Load(); n != 0 {
		w.violatef("parked counter = %d after stop", n)
	}
	for _, wk := range s.workerSnapshot() {
		if wk.parked.Load() {
			w.violatef("worker %d still counted as parked", wk.id)
		}
	}
	if n := s.processorCount.Load(); n != 0 {
		w.violatef("processor count = %d after stop", n)
	}
	s.admitMu.Lock()
	if s.admitting != 0 {
		w.violatef("admissions in flight = %d after stop", s.admitting)
	}
	s.admitMu.Unlock()
	n := 0
	s.byPID.Range(func(_, _ any) bool { n++; return true })
	if n != 0 {
		w.violatef("%d processors still registered by pid", n)
	}
	n = 0
	s.byQueue.Range(func(_, _ any) bool { n++; return true })
	if n != 0 {
		w.violatef("%d processors still registered by queue", n)
	}
	if l := s.global.Len(); l > 0 {
		w.violatef("global queue holds %d entries after stop", l)
	}

	if len(w.violations) > 0 {
		t.Errorf("seed %d: %d invariant violations (replay: WIPPY_CHAOS_SEED=%d WIPPY_CHAOS_RUNS=1):\n  %s",
			seed, len(w.violations), seed, strings.Join(w.violations, "\n  "))
	}
}

// checkOutcome verifies the process ended the way its plan and the external
// events that hit it allow.
func (w *chaosWorld) checkOutcome(rec *chaosRec, res *apiruntime.Result) {
	if res == nil {
		w.violatef("rec %d: no result", rec.id)
		return
	}
	err := res.Error
	killed := rec.killed.Load() || w.stopped.Load()
	switch {
	case err == nil:
		var tok chaosToken
		var ok bool
		if res.Value != nil {
			tok, ok = res.Value.Data().(chaosToken)
		}
		if !ok || tok.rec != rec.id || tok.inc != len(rec.plans)-1 {
			w.violatef("rec %d: result %v does not come from the final incarnation %d", rec.id, res.Value, len(rec.plans)-1)
		}
		last := rec.plans[len(rec.plans)-1]
		if k := last[len(last)-1].kind; k == opFail || k == opNever {
			w.violatef("rec %d: finished although its plan ends with kind %d", rec.id, k)
		}
	case errors.Is(err, process.ErrStepLimitExceeded):
		if !rec.expectsLimit() {
			w.violatef("rec %d: step limit error without max_steps", rec.id)
		}
	case errors.Is(err, errChaosFail):
		last := rec.plans[len(rec.plans)-1]
		if last[len(last)-1].kind != opFail {
			w.violatef("rec %d: planned failure but plan does not fail", rec.id)
		}
	case errors.Is(err, sysprocess.ErrTerminated), errors.Is(err, errChaosCancel), errors.Is(err, context.Canceled):
		if !killed {
			w.violatef("rec %d: terminated without a terminate or stop (%v)", rec.id, err)
		}
	case errors.Is(err, errChaosStart):
		if rec.failUpgrade == 0 && !rec.failUpInit {
			w.violatef("rec %d: upgrade failure without injected failure (%v)", rec.id, err)
		}
	default:
		w.violatef("rec %d: unexpected error %v", rec.id, err)
	}
	if err != nil && !killed && !rec.expectsLimit() && rec.failUpgrade == 0 && !rec.failUpInit {
		last := rec.plans[len(rec.plans)-1]
		if last[len(last)-1].kind != opFail {
			w.violatef("rec %d: unprovoked failure %v", rec.id, err)
		}
	}
	if err == nil && rec.blocker && !killed {
		w.violatef("rec %d: blocker finished", rec.id)
	}
}

func outcomeName(res *apiruntime.Result) string {
	switch err := res.Error; {
	case err == nil:
		return "done"
	case errors.Is(err, process.ErrStepLimitExceeded):
		return "limit"
	case errors.Is(err, errChaosFail):
		return "fail"
	case errors.Is(err, errChaosStart):
		return "upgrade-fail"
	default:
		return "killed"
	}
}
