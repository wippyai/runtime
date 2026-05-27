// SPDX-License-Identifier: MPL-2.0

package determinism

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/system/eventbus"
	sysregistry "github.com/wippyai/runtime/system/registry"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/runner"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

const (
	providerKind = "mre.provider"
	consumerKind = "mre.consumer"
)

var (
	flagRuns     = flag.Int("runs", 2000, "number of boot iterations per variant")
	flagOutDir   = flag.String("out", "", "directory to write per-iteration JSON results into; empty disables")
	flagBaseSeed = flag.Uint64("seed", 0xC0FFEE, "base seed for the per-iteration input shuffle (0 = use time)")
)

type iterResult struct {
	Variant   string `json:"variant"`
	Outcome   string `json:"outcome"`
	Reason    string `json:"reason,omitempty"`
	InputOrd  string `json:"input_order"`
	Duration  string `json:"duration"`
	Iteration int    `json:"iteration"`
}

type sharedResources struct {
	registered map[string]struct{}
	mu         sync.Mutex
}

func newSharedResources() *sharedResources {
	return &sharedResources{registered: make(map[string]struct{})}
}

func (s *sharedResources) register(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registered[id] = struct{}{}
}

func (s *sharedResources) has(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.registered[id]
	return ok
}

type providerListener struct{ res *sharedResources }

func (p *providerListener) Add(_ context.Context, e registry.Entry) error {
	p.res.register(e.ID.String())
	if traceListeners {
		fmt.Fprintf(os.Stderr, "[provider] Add id=%s\n", e.ID.String())
	}
	return nil
}

func (p *providerListener) Update(ctx context.Context, e registry.Entry) error {
	return p.Add(ctx, e)
}

func (p *providerListener) Delete(_ context.Context, _ registry.Entry) error { return nil }

type consumerListener struct{ res *sharedResources }

func (c *consumerListener) Add(_ context.Context, e registry.Entry) error {
	target, _ := extractProviderRef(e)
	if traceListeners {
		fmt.Fprintf(os.Stderr, "[consumer] Add id=%s target=%q registered=%v\n", e.ID.String(), target, c.res.has(target))
	}
	if target == "" {
		return errors.New("consumer entry missing provider ref")
	}
	if !c.res.has(target) {
		return apierror.New(apierror.NotFound, "filesystem not found: "+target).WithRetryable(apierror.False)
	}
	return nil
}

var traceListeners = os.Getenv("MRE_TRACE") == "1"

func (c *consumerListener) Update(ctx context.Context, e registry.Entry) error {
	return c.Add(ctx, e)
}

func (c *consumerListener) Delete(_ context.Context, _ registry.Entry) error { return nil }

func extractProviderRef(e registry.Entry) (string, bool) {
	if e.Data == nil {
		return "", false
	}
	d, ok := e.Data.Data().(map[string]any)
	if !ok {
		return "", false
	}
	v, ok := d["provider"].(string)
	return v, ok
}

type variant struct {
	name         string
	providerID   registry.ID
	consumerID   registry.ID
	expectsAlpha string
}

func variantA() variant {
	return variant{
		name:         "A_provider_lex_first",
		providerID:   registry.NewID("mre", "aaa.driver"),
		consumerID:   registry.NewID("mre", "zzz.client"),
		expectsAlpha: "provider sorts first",
	}
}

func variantB() variant {
	return variant{
		name:         "B_consumer_lex_first",
		providerID:   registry.NewID("mre", "zzz.driver"),
		consumerID:   registry.NewID("mre", "aaa.client"),
		expectsAlpha: "consumer sorts first",
	}
}

func (v variant) changeset() registry.ChangeSet {
	return registry.ChangeSet{
		{
			Kind: registry.EntryCreate,
			Entry: registry.Entry{
				ID:   v.providerID,
				Kind: providerKind,
				Data: payload.New(map[string]any{}),
			},
		},
		{
			Kind: registry.EntryCreate,
			Entry: registry.Entry{
				ID:   v.consumerID,
				Kind: consumerKind,
				Data: payload.New(map[string]any{"provider": v.providerID.String()}),
			},
		},
	}
}

type harness struct {
	registry registry.Registry
	router   *eventbus.EventRouter
	awaitSvc *eventbus.AwaitService
	ctx      context.Context
	cancel   context.CancelFunc
}

func buildHarness(t testing.TB) *harness {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())

	bus := eventbus.NewBus()
	ctx = event.WithBus(ctx, bus)

	awaitSvc := eventbus.NewAwaitService(bus)
	ctx = event.WithAwaitService(ctx, awaitSvc)
	if err := awaitSvc.Start(ctx); err != nil {
		cancel()
		t.Fatalf("start await service: %v", err)
	}

	res := newSharedResources()
	hreg := boot.NewHandlerRegistry()
	hreg.RegisterListener(providerKind, &providerListener{res: res})
	hreg.RegisterListener(consumerKind, &consumerListener{res: res})

	log := zap.NewNop()

	router, err := eventbus.StartRouter(ctx, bus, eventbus.WithHandlers(hreg.Handlers()...))
	if err != nil {
		cancel()
		t.Fatalf("start router: %v", err)
	}

	resolver := topology.NewResolver()
	builder := topology.NewStateBuilder(log, resolver)
	runr := runner.NewBusRunner(bus, log, builder,
		runner.WithTransactionParticipants(hreg.TransactionParticipants),
		runner.WithEventWaitTimeout(2*time.Second),
	)
	reg := sysregistry.NewRegistry(historymem.New(), runr, builder, resolver, log)

	return &harness{
		registry: reg,
		router:   router,
		awaitSvc: awaitSvc,
		ctx:      ctx,
		cancel:   cancel,
	}
}

func (h *harness) close() {
	if h.router != nil {
		_ = h.router.Stop()
	}
	if h.awaitSvc != nil {
		_ = h.awaitSvc.Stop()
	}
	if h.cancel != nil {
		h.cancel()
	}
}

func shuffle(cs registry.ChangeSet, rng *rand.Rand) registry.ChangeSet {
	out := make(registry.ChangeSet, len(cs))
	copy(out, cs)
	rng.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

func orderTag(cs registry.ChangeSet) string {
	ids := make([]string, len(cs))
	for i, op := range cs {
		ids[i] = op.Entry.ID.String()
	}
	return strings.Join(ids, ",")
}

func runIteration(t testing.TB, v variant, rng *rand.Rand, iter int) iterResult {
	t.Helper()

	h := buildHarness(t)
	defer h.close()

	cs := shuffle(v.changeset(), rng)
	order := orderTag(cs)

	ctx, cancel := context.WithTimeout(h.ctx, 5*time.Second)
	defer cancel()

	start := time.Now()
	_, err := h.registry.Apply(ctx, cs)
	dur := time.Since(start)

	res := iterResult{
		Iteration: iter,
		Variant:   v.name,
		Duration:  dur.String(),
		InputOrd:  order,
	}
	if err != nil {
		res.Outcome = "fail"
		res.Reason = err.Error()
		return res
	}
	res.Outcome = "pass"
	return res
}

type summary struct {
	Reasons map[string]int
	Variant string
	N       int
	Pass    int
	Fail    int
}

func runVariant(t *testing.T, v variant, runs int, baseSeed uint64) summary {
	t.Helper()

	rng := rand.New(rand.NewPCG(baseSeed, uint64(time.Now().UnixNano())))
	sum := summary{Variant: v.name, N: runs, Reasons: make(map[string]int)}

	var writer *resultWriter
	if dir := *flagOutDir; dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create out dir: %v", err)
		}
		f, err := os.Create(fmt.Sprintf("%s/%s.jsonl", dir, v.name))
		if err != nil {
			t.Fatalf("create result file: %v", err)
		}
		defer f.Close()
		writer = &resultWriter{f: f}
	}

	for i := 0; i < runs; i++ {
		res := runIteration(t, v, rng, i)
		if writer != nil {
			writer.write(res)
		}
		if res.Outcome == "pass" {
			sum.Pass++
			continue
		}
		sum.Fail++
		key := classifyReason(res.Reason)
		sum.Reasons[key]++
		if key == "other" && sum.Fail <= 3 {
			t.Logf("FAIL_DETAIL iter=%d order=%s reason=%s", res.Iteration, res.InputOrd, res.Reason)
		}
	}
	return sum
}

type resultWriter struct {
	f   *os.File
	enc *json.Encoder
	mu  sync.Mutex
}

func (w *resultWriter) write(r iterResult) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.enc == nil {
		w.enc = json.NewEncoder(w.f)
	}
	_ = w.enc.Encode(r)
}

func classifyReason(reason string) string {
	switch {
	case strings.Contains(reason, "filesystem not found"):
		return "filesystem_not_found"
	case strings.Contains(reason, "driver not found"):
		return "driver_not_found"
	case strings.Contains(reason, "deadline"), strings.Contains(reason, "timeout"):
		return "timeout"
	default:
		return "other"
	}
}

func TestPR270_BootDeterminism_VariantA(t *testing.T) {
	if testing.Short() {
		t.Skip("skip in short mode")
	}
	sum := runVariant(t, variantA(), *flagRuns, *flagBaseSeed)
	t.Logf("VARIANT_SUMMARY %s N=%d pass=%d fail=%d reasons=%v",
		sum.Variant, sum.N, sum.Pass, sum.Fail, sum.Reasons)
}

func TestPR270_BootDeterminism_VariantB(t *testing.T) {
	if testing.Short() {
		t.Skip("skip in short mode")
	}
	sum := runVariant(t, variantB(), *flagRuns, *flagBaseSeed+1)
	t.Logf("VARIANT_SUMMARY %s N=%d pass=%d fail=%d reasons=%v",
		sum.Variant, sum.N, sum.Pass, sum.Fail, sum.Reasons)
}
