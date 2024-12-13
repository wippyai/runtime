package registry

import (
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
)

const (
	System events.System = "registry"

	Create events.Kind = "entry.create"
	Update events.Kind = "entry.update"
	Delete events.Kind = "entry.delete"
	Accept events.Kind = "entry.accept"
	Reject events.Kind = "entry.reject"

	RootVersion uint = 0
)

type (
	Path     string
	Kind     string
	Metadata map[string]any

	Version interface {
		ID() uint
		Previous() Version // nil for root version
		String() string
	}

	State []Entry
	Entry struct {
		Path Path
		Kind Kind
		Meta Metadata
		Data payload.Payload
	}

	ChangeSet []Operation
	Operation struct {
		Kind  events.Kind
		Entry Entry
	}

	Registry interface {
		EntryReader
		StateWriter
		Active() (Version, error)
	}

	StateWriter interface {
		Apply(State) (Version, error)
		ApplyVersion(Version) error
	}

	Builder interface {
		BuildState(History, Version) (State, error)
		BuildDelta(History, Version, Version) (ChangeSet, error)
	}

	EntryReader interface {
		GetAllEntries() ([]Entry, error)
		GetEntry(Path) (Entry, error)
	}

	History interface {
		Versions() ([]Version, error)
		Get(Version) (ChangeSet, error)
		Save(v Version, cs ChangeSet, head bool) error
		Head() (Version, error)
	}

	Loader interface {
		Register(prefix Path, payload ...payload.Payload) error
		Entries() []Entry
		Reset()
	}
)
