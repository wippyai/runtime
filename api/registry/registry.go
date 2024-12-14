package registry

import (
	"context"
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
	// Path represents a unique identifier for a registry entry.
	// It typically uses a hierarchical structure (e.g., "service.database.url").
	Path string

	// Kind is a string representing the type of an entry (e.g., "config", "service", "endpoint").
	// This helps categorize entries for different purposes.
	Kind string

	// Metadata is a map for storing arbitrary key-value metadata associated with an entry.
	// This can include any additional information relevant to the entry.
	Metadata map[string]any

	// Version represents a specific version of the registry's state.
	Version interface {
		// ID returns the unique identifier of the version. This is typically an auto-incrementing number.
		ID() uint
		// Previous returns the previous Version, or nil if this is the root version.
		Previous() Version
		// String returns a string representation of the version.
		String() string
	}

	// State represents the complete state of the registry at a specific version.
	State []Entry

	// Entry represents a single entry in the registry.
	Entry struct {
		// Path is the unique identifier for the entry.
		Path Path
		// Kind is the type/category of the entry.
		Kind Kind
		// Meta contains any additional metadata about the entry.
		Meta Metadata
		// Data is the actual payload associated with the entry, providing its value or configuration.
		Data payload.Payload
	}

	// ChangeSet represents a set of operations that, when applied sequentially, transition the registry from one state to another.
	ChangeSet []Operation

	// Operation represents a single operation within a ChangeSet (e.g., create, update, or delete an entry).
	Operation struct {
		// Kind is the type of operation.
		Kind events.Kind
		// Entry is the entry affected by the operation. For Delete operations, only the Path field might be relevant.
		Entry Entry
	}

	// Registry is the primary interface for interacting with the registry.
	// It combines methods for reading entries, applying changes, and getting the current version.
	Registry interface {
		EntryReader
		StateWriter
		// Current returns the current version of the registry's state.
		Current() (Version, error)
	}

	// StateWriter defines methods for applying changes to the registry's state.
	StateWriter interface {
		// Apply applies a ChangeSet to the registry, creating a new version with the modified state.
		// It returns the newly created version.
		Apply(context.Context, ChangeSet) (Version, error)
		// ApplyVersion applies a specific version to the registry.
		// This effectively rolls the registry's state back or forward to the specified version.
		ApplyVersion(context.Context, Version) error
	}

	// StateBuilder defines methods for constructing registry states and calculating the differences between versions.
	StateBuilder interface {
		// BuildState constructs the complete registry state at a specific version by applying all changes from the root version up to the target version.
		BuildState(History, Version) (State, error)
		// BuildDelta calculates the minimal ChangeSet required to transition the registry from one version to another.
		BuildDelta(State, State) (ChangeSet, error)
	}

	// EntryReader defines methods for reading entries from the registry.
	EntryReader interface {
		// GetAllEntries retrieves all entries in the registry's current state.
		GetAllEntries() ([]Entry, error)
		// GetEntry retrieves a specific entry by its path. Returns an error if the entry is not found.
		GetEntry(Path) (Entry, error)
	}

	// History defines methods for managing the version history of the registry.
	// It allows retrieving past versions, storing new versions, and navigating the version timeline.
	History interface {
		// Versions returns a list of all versions available in the history.
		Versions() ([]Version, error)
		// Get retrieves the ChangeSet associated with a specific version.
		Get(Version) (ChangeSet, error)
		// Save records a new version and its associated ChangeSet in the history.
		// The 'head' parameter indicates whether this new version should become the head (current) version.
		Save(v Version, cs ChangeSet, head bool) error
		// Head returns the current head version of the history.
		Head() (Version, error)
	}

	// Runner defines how ChangeSets are applied to a State to produce a new State.
	// It encapsulates the logic for handling different operation kinds. This component propagates whole
	// system state.
	Runner interface {
		// Transition applies a given ChangeSet to a State and returns the resulting modified State.
		Transition(context.Context, State, ChangeSet) (State, error)
	}
)
