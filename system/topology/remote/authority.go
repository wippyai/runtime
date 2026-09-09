// SPDX-License-Identifier: MPL-2.0

// Package remote provides native, host-owned authorization for remote topology
// monitoring. It deliberately does not route control messages or mutate
// topology state.
package remote

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

var (
	// ErrInvalidGrantSpec means a grant specification is not an exact,
	// canonical remote-watcher to local-target permission.
	ErrInvalidGrantSpec = errors.New("remote topology: invalid monitor grant specification")
	// ErrGrantCapacity means the authority has reached its configured live grant limit.
	ErrGrantCapacity = errors.New("remote topology: monitor grant capacity reached")
	// ErrGrantDenied means a token is unknown or does not match its exact grant tuple.
	ErrGrantDenied = errors.New("remote topology: monitor grant denied")
	// ErrGrantExpired means the grant reached its fixed expiry.
	ErrGrantExpired = errors.New("remote topology: monitor grant expired")
	// ErrGrantRevoked means the owning host revoked the grant.
	ErrGrantRevoked = errors.New("remote topology: monitor grant revoked")
	// ErrAuthorityClosed means the owning host closed its grant authority.
	ErrAuthorityClosed = errors.New("remote topology: monitor grant authority closed")
	// ErrIngressUnauthorized means the transport did not authenticate and protect
	// the immediate peer.
	ErrIngressUnauthorized = errors.New("remote topology: ingress is not authenticated and integrity protected")
	// ErrIngressClosed means the exact ingress connection is absent or already closed.
	ErrIngressClosed = errors.New("remote topology: ingress connection is closed")
	// ErrIngressConnectionChanged means a grant already belongs to a different
	// exact ingress connection. Reconnection requires a new grant.
	ErrIngressConnectionChanged = errors.New("remote topology: monitor grant is bound to another connection")
	// ErrNilUseCallback means Lease.Use received no admission callback.
	ErrNilUseCallback = errors.New("remote topology: nil use callback")
)

// Token is an opaque, cryptographically random 256-bit monitor grant.
// It has no identity or authority outside the Authority that minted it.
type Token [32]byte

// Bytes returns a copy suitable for a typed control envelope.
func (t Token) Bytes() []byte {
	b := make([]byte, len(t))
	copy(b, t[:])
	return b
}

// String returns the canonical 64-character lowercase hexadecimal wire form.
func (t Token) String() string { return hex.EncodeToString(t[:]) }

// ParseToken accepts exactly one canonical 256-bit hexadecimal monitor-grant
// encoding. It intentionally rejects alternate encodings so the control layer
// has one stable representation for deduplication and audit records.
func ParseToken(encoded string) (Token, error) {
	var token Token
	if len(encoded) != hex.EncodedLen(len(token)) || encoded != strings.ToLower(encoded) {
		return token, ErrGrantDenied
	}
	if _, err := hex.Decode(token[:], []byte(encoded)); err != nil {
		return Token{}, ErrGrantDenied
	}
	return token, nil
}

// GrantSpec is an immutable, exact permission once granted. Watcher and Target
// must be fully-qualified canonical actor PIDs. A grant authorizes monitoring
// only; it cannot authorize linking or any other topology operation.
type GrantSpec struct {
	Expires  time.Time
	PeerNode pid.NodeID
	Watcher  pid.PID
	Target   pid.PID
}

// Authority is a native owner-side table of bounded remote-monitor grants.
// Construct it once for a fixed local node. It is intentionally not a relay
// receiver, registry, or Lua-facing API.
type Authority struct {
	grants     map[Token]*grant
	closing    chan struct{}
	closedDone chan struct{}
	localNode  pid.NodeID
	capacity   int
	mu         sync.Mutex
	closed     bool
}

// NewAuthority creates an authority for localNode with at most capacity live
// grants. A capacity must be positive; expired, revoked, or closed grants are
// reclaimed before a later grant consumes capacity.
func NewAuthority(localNode pid.NodeID, capacity int) (*Authority, error) {
	if !validComponent(localNode) || capacity <= 0 {
		return nil, ErrInvalidGrantSpec
	}
	return &Authority{
		localNode:  localNode,
		capacity:   capacity,
		grants:     make(map[Token]*grant, capacity),
		closing:    make(chan struct{}),
		closedDone: make(chan struct{}),
	}, nil
}

// Grant mints an opaque capability for exactly spec. Expiry is fixed at mint
// time; callers cannot alter a live grant by mutating their input value later.
func (a *Authority) Grant(spec GrantSpec) (Token, error) {
	var zero Token
	canonical, err := a.canonicalSpec(spec, time.Now())
	if err != nil {
		return zero, err
	}

	a.reclaimExpired(time.Now())

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return zero, ErrAuthorityClosed
	}
	if len(a.grants) >= a.capacity {
		return zero, ErrGrantCapacity
	}

	for {
		var token Token
		if _, err := rand.Read(token[:]); err != nil {
			return zero, err
		}
		if _, exists := a.grants[token]; exists {
			continue
		}

		g := &grant{
			token:  token,
			spec:   canonical,
			active: true,
			done:   make(chan struct{}),
		}
		a.grants[token] = g
		g.armExpiry(a)
		return token, nil
	}
}

// Revoke removes token's permission. It is idempotent: false means no live
// grant remained. It waits for an already-admitted Use callback, so it cannot
// return while that callback can still install a monitor relationship.
func (a *Authority) Revoke(token Token) bool {
	a.mu.Lock()
	g, ok := a.grants[token]
	a.mu.Unlock()
	if !ok {
		return false
	}
	return a.deactivate(g, ErrGrantRevoked)
}

// Close revokes every grant. It is idempotent and waits for already-admitted
// callbacks before returning.
func (a *Authority) Close() {
	a.mu.Lock()
	if a.closed {
		done := a.closedDone
		a.mu.Unlock()
		<-done
		return
	}
	a.closed = true
	close(a.closing)
	grants := make([]*grant, 0, len(a.grants))
	for _, g := range a.grants {
		grants = append(grants, g)
	}
	a.mu.Unlock()

	for _, g := range grants {
		a.deactivate(g, ErrAuthorityClosed)
	}
	close(a.closedDone)
}

// Acquire validates an inbound remote-monitor request. It does not install a
// monitor; callers must do that only inside Lease.Use. The first successful
// acquire binds the grant to ingress.ConnectionClosed. A later connection,
// including a reconnect from the same authenticated node and actor, is denied.
func (a *Authority) Acquire(token Token, ingress relay.IngressIdentity, watcher, target pid.PID) (*Lease, error) {
	if a.isClosing() {
		return nil, ErrAuthorityClosed
	}
	if !ingress.Authenticated || !ingress.IntegrityProtected || ingress.Node == "" {
		return nil, ErrIngressUnauthorized
	}
	if connectionClosed(ingress.ConnectionClosed) {
		return nil, ErrIngressClosed
	}

	a.mu.Lock()
	g, ok := a.grants[token]
	a.mu.Unlock()
	if !ok {
		return nil, ErrGrantDenied
	}

	if !samePID(watcher, g.spec.Watcher) || !samePID(target, g.spec.Target) ||
		ingress.Node != g.spec.PeerNode || watcher.Node != ingress.Node {
		return nil, ErrGrantDenied
	}

	bound, err := g.bind(ingress.ConnectionClosed, time.Now(), a.closing)
	if err != nil {
		if errors.Is(err, ErrGrantExpired) {
			a.deactivate(g, ErrGrantExpired)
		}
		if errors.Is(err, ErrIngressClosed) {
			a.deactivate(g, ErrIngressClosed)
		}
		return nil, err
	}
	if bound {
		go g.watchConnection(a, ingress.ConnectionClosed)
	}
	return &Lease{authority: a, grant: g, connection: ingress.ConnectionClosed}, nil
}

// Lease is a successfully acquired, connection-bound permission. Done closes
// after revocation, expiry, authority close, or closure of that exact ingress
// connection. It is suitable for cleanup notification, not as proof that an
// installation succeeded.
type Lease struct {
	authority  *Authority
	grant      *grant
	connection <-chan struct{}
}

// Done returns the grant's cleanup signal. A nil lease has no signal.
func (l *Lease) Done() <-chan struct{} {
	if l == nil || l.grant == nil {
		return nil
	}
	return l.grant.done
}

// Use executes install only while this lease remains live. It is the required
// admission gate around a topology mutation. Use never holds the Authority
// mutex while callback runs, but callbacks must be short, non-blocking native
// work: do not perform I/O or wait for another actor. A callback must not call
// any Authority method, wait on this lease's Done channel, or call Use on this
// lease again. Those operations can wait for this admission callback's gate.
func (l *Lease) Use(callback func() error) error {
	if l == nil || l.authority == nil || l.grant == nil {
		return ErrGrantDenied
	}
	if callback == nil {
		return ErrNilUseCallback
	}
	// Avoid waiting behind Close's pending writer once close has begun. The
	// second check below makes the closing signal part of the serialized gate.
	if l.authority.isClosing() {
		return ErrAuthorityClosed
	}

	g := l.grant
	g.gate.RLock()
	if !g.active {
		err := g.reason
		g.gate.RUnlock()
		if err == nil {
			return ErrGrantDenied
		}
		return err
	}
	if l.authority.isClosing() {
		g.gate.RUnlock()
		return ErrAuthorityClosed
	}
	if !g.spec.Expires.After(time.Now()) {
		g.gate.RUnlock()
		l.authority.deactivate(g, ErrGrantExpired)
		return ErrGrantExpired
	}
	if g.connection != l.connection {
		g.gate.RUnlock()
		return ErrIngressConnectionChanged
	}
	if connectionClosed(l.connection) {
		g.gate.RUnlock()
		l.authority.deactivate(g, ErrIngressClosed)
		return ErrIngressClosed
	}

	// The read gate stays held through callback. deactivate takes the write gate,
	// so revocation, expiry, close, and connection loss cannot complete while an
	// admitted installation is still running.
	defer g.gate.RUnlock()
	return callback()
}

type canonicalGrantSpec struct {
	Expires  time.Time
	PeerNode pid.NodeID
	Watcher  pid.PID
	Target   pid.PID
}

type grant struct {
	reason     error
	connection <-chan struct{}
	done       chan struct{}
	timer      *time.Timer
	spec       canonicalGrantSpec
	token      Token
	gate       sync.RWMutex
	active     bool
}

func (g *grant) armExpiry(a *Authority) {
	g.gate.Lock()
	g.timer = time.AfterFunc(time.Until(g.spec.Expires), func() {
		a.deactivate(g, ErrGrantExpired)
	})
	g.gate.Unlock()
}

// bind returns true only for the first accepted connection binding.
func (g *grant) bind(connection <-chan struct{}, now time.Time, closing <-chan struct{}) (bool, error) {
	g.gate.Lock()
	defer g.gate.Unlock()
	select {
	case <-closing:
		return false, ErrAuthorityClosed
	default:
	}
	if !g.active {
		if g.reason != nil {
			return false, g.reason
		}
		return false, ErrGrantDenied
	}
	if !g.spec.Expires.After(now) {
		return false, ErrGrantExpired
	}
	if connectionClosed(connection) {
		return false, ErrIngressClosed
	}
	if g.connection == nil {
		g.connection = connection
		return true, nil
	}
	if g.connection != connection {
		return false, ErrIngressConnectionChanged
	}
	return false, nil
}

func (g *grant) watchConnection(a *Authority, connection <-chan struct{}) {
	select {
	case <-connection:
		a.deactivate(g, ErrIngressClosed)
	case <-g.done:
	}
}

func (a *Authority) deactivate(g *grant, reason error) bool {
	if g == nil {
		return false
	}

	// This writer gate waits for an admitted Use callback but does not hold the
	// authority map lock while it waits or while external code is running.
	g.gate.Lock()
	if !g.active {
		g.gate.Unlock()
		return false
	}
	g.active = false
	g.reason = reason
	if g.timer != nil {
		g.timer.Stop()
	}
	g.gate.Unlock()

	a.mu.Lock()
	if current, ok := a.grants[g.token]; ok && current == g {
		delete(a.grants, g.token)
	}
	a.mu.Unlock()
	// Done is a cleanup signal. Publish it only after a sequential Grant can
	// observe reclaimed capacity, rather than exposing a transient full table.
	close(g.done)
	return true
}

func (a *Authority) reclaimExpired(now time.Time) {
	a.mu.Lock()
	grants := make([]*grant, 0, len(a.grants))
	for _, g := range a.grants {
		grants = append(grants, g)
	}
	a.mu.Unlock()

	for _, g := range grants {
		if !g.spec.Expires.After(now) {
			a.deactivate(g, ErrGrantExpired)
		}
	}
}

func (a *Authority) canonicalSpec(spec GrantSpec, now time.Time) (canonicalGrantSpec, error) {
	if !validComponent(spec.PeerNode) || spec.PeerNode == a.localNode || !spec.Expires.After(now) {
		return canonicalGrantSpec{}, ErrInvalidGrantSpec
	}
	if !validActorPID(spec.Watcher) || !validActorPID(spec.Target) ||
		spec.Watcher.Node != spec.PeerNode || spec.Target.Node != a.localNode {
		return canonicalGrantSpec{}, ErrInvalidGrantSpec
	}
	return canonicalGrantSpec{
		PeerNode: spec.PeerNode,
		Watcher:  canonicalPID(spec.Watcher),
		Target:   canonicalPID(spec.Target),
		Expires:  spec.Expires,
	}, nil
}

// canonicalPID deliberately reads exported fields rather than PID.String(),
// whose cached representation may be stale after a caller mutates a PID.
func canonicalPID(p pid.PID) pid.PID {
	return pid.PID{Node: p.Node, Host: p.Host, UniqID: p.UniqID}
}

func samePID(a, b pid.PID) bool {
	return a.Node == b.Node && a.Host == b.Host && a.UniqID == b.UniqID
}

func validActorPID(p pid.PID) bool {
	return validComponent(p.Node) && validComponent(p.Host) && validComponent(p.UniqID)
}

func validComponent(value string) bool {
	if value == "" || len(value) > 160 || !utf8.ValidString(value) || strings.ContainsAny(value, "{}@|\\\"") {
		return false
	}
	for _, r := range value {
		if r <= 32 || r == 127 || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func connectionClosed(connection <-chan struct{}) bool {
	if connection == nil {
		return true
	}
	select {
	case <-connection:
		return true
	default:
		return false
	}
}

func (a *Authority) isClosing() bool {
	select {
	case <-a.closing:
		return true
	default:
		return false
	}
}
