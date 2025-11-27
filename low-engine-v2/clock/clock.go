package clock

import "time"

// Clock abstracts time operations for testing
type Clock interface {
	Now() time.Time
	Sleep(d time.Duration)
}

// Real implements Clock using actual system time
type Real struct{}

func NewReal() *Real {
	return &Real{}
}

func (r *Real) Now() time.Time {
	return time.Now()
}

func (r *Real) Sleep(d time.Duration) {
	time.Sleep(d)
}

// Mock implements Clock with controllable time for testing
type Mock struct {
	current time.Time
}

func NewMock(start time.Time) *Mock {
	return &Mock{current: start}
}

func (m *Mock) Now() time.Time {
	return m.current
}

func (m *Mock) Sleep(d time.Duration) {
	m.current = m.current.Add(d)
}

func (m *Mock) Advance(d time.Duration) {
	m.current = m.current.Add(d)
}

func (m *Mock) Set(t time.Time) {
	m.current = t
}
