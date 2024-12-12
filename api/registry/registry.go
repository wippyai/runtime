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
)

type (
	Path     string
	Kind     string
	Metadata map[string]any

	// VersionHistory represents a collection of Version instances that may form a branched
	// version storage. The underlying data structure is based on a map[string]Version,
	// where the key is the Version.ID().
	VersionHistory interface {
		// Path returns the ordered sequence of Version instances connecting a starting
		// version ('from') and an ending version ('to'), including both 'from' and 'to'.
		//
		// Constraints:
		//   - Both 'from' and 'to' MUST exist; otherwise, nil is returned.
		//   - If 'from' and 'to' are identical, the returned slice contains only 'from'.
		//   - If no valid path exists, nil is returned.
		Path(from, to Version) ([]Version, error)

		// Len returns the number of versions.
		Len() int

		// Add adds a new version. Duplicate versions not allowed.
		Add(v Version) error

		// Range iterates over the versions.
		Range(f func(id string, v Version) bool)
	}

	Version interface {
		ID() string
		PreviousID() string // empty for root version
		Major() uint
		Minor() uint
	}

	State []Entry
	Entry struct {
		Path Path
		Kind Kind
		Meta Metadata
		Data payload.Payload
	}

	OperationSet []Action
	Action       struct {
		Kind  events.Kind
		Entry Entry
	}

	Storage interface {
		Versions() ([]Version, error)
		Get(Version) (OperationSet, error)
		Save(Version, OperationSet) error
	}

	Registry interface {
		Apply(OperationSet) (Version, error)
		Versions() ([]Version, error)
		GetActions(Version) (OperationSet, error)
		Head() (Version, error)
	}

	StateSeeker interface {
		GetState(Version) (State, error)
	}

	EntryReader interface {
		GetEntry(Path) (Entry, error)
	}

	Activator interface {
		Activate(Version) error
		Current() (Version, error)
	}

	Loader interface {
		WithPrefix(Path) Loader
		Load(...payload.Payload) error
		Entries() []Entry
		Reset()
	}
)
