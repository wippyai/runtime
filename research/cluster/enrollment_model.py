"""Bounded enrollment interleavings; design evidence, not a runtime test.

One entering participant and one Strong reservation. Separates inventory read
from reservation commit so removing the CAS guard produces a counterexample.
No clocks, network assumptions, or scheduling fairness are used for safety.
This model does not cover retirement, incarnation replacement, or name reuse.
"""
from collections import deque
from dataclasses import dataclass, replace


@dataclass(frozen=True)
class State:
    enrolled: bool = False
    inventory_read: int = -1
    phase: str = "absent"
    required: bool = False
    snapshot: str = "uncaptured"
    installed: bool = False
    ready: bool = False
    exclusion: bool = False
    ack: bool = False


def transitions(s, guarded):
    if not s.enrolled:
        yield "commit enrollment", replace(s, enrolled=True)
    if s.phase == "absent":
        if s.inventory_read != int(s.enrolled):
            yield "read participant inventory", replace(s, inventory_read=int(s.enrolled))
        if s.inventory_read >= 0 and (not guarded or s.inventory_read == int(s.enrolled)):
            yield "commit reservation", replace(s, phase="pending", required=bool(s.inventory_read))
    if s.enrolled and s.snapshot == "uncaptured":
        yield "capture authoritative snapshot", replace(s, snapshot=s.phase)
    if s.snapshot != "uncaptured" and not s.installed:
        yield "install complete snapshot", replace(s, installed=True, exclusion=s.snapshot in ("pending", "active"))
    if s.installed and not s.ready:
        yield "open local admission", replace(s, ready=True)
    if s.phase == "pending":
        if s.installed and s.required and not s.ack:
            yield "install exclusion and acknowledge", replace(s, exclusion=True, ack=True)
        if not s.required or s.ack:
            yield "promote", replace(s, phase="active")
        yield "cancel reservation", replace(s, phase="terminal")


def explore(guarded):
    initial = State()
    queue = deque([(initial, [])])
    seen = {initial}
    while queue:
        state, trace = queue.popleft()
        if state.ready and state.phase == "active" and not state.exclusion:
            return len(seen), trace
        for event, next_state in transitions(state, guarded):
            if next_state not in seen:
                seen.add(next_state)
                queue.append((next_state, trace + [event]))
    return len(seen), None


if __name__ == "__main__":
    count, failure = explore(True)
    assert failure is None, failure
    print(f"Guarded enrollment: {count} reachable states; no uncovered active claim")
    count, failure = explore(False)
    assert failure is not None, "negative control did not expose missing inventory CAS"
    print("Without inventory CAS: " + " -> ".join(failure))
