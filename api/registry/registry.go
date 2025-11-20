// Package registry provides a versioned storage system for configuration entries.
package registry

import (
	"context"

	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
)

// Registry system constants define the various event types and identifiers used throughout the registry.
const (
	// System identifies the registry system in the event bus
	System event.System = "registry"

	// Create represents an event for creating a new registry entry
	Create event.Kind = "entry.create"
	// Update represents an event for updating an existing registry entry
	Update event.Kind = "entry.update"
	// Delete represents an event for removing a registry entry
	Delete event.Kind = "entry.delete"

	// Accept represents an event for accepting a registry entry
	Accept event.Kind = "entry.accept"
	// Reject represents an event for rejecting a registry entry
	Reject event.Kind = "entry.reject"

	// Begin represents the start of a registry transaction
	Begin event.Kind = "registry.begin"
	// Commit represents the successful completion of a registry transaction
	Commit event.Kind = "registry.commit"
	// Discard represents the rollback of a registry transaction
	Discard event.Kind = "registry.discard"

	// Changes represents a pattern matching any create, update, or delete events
	Changes event.Kind = "entry.(create|update|delete)"
	// AllEvents matches any registry operation event
	AllEvents event.Kind = "(entry|registry).(create|update|delete|begin|commit|discard)"

	// RootVersion represents the initial version of the registry
	RootVersion uint = 0

	// TagDependsOn is used to mark dependencies between registry entries, groups and ns
	TagDependsOn = "depends_on"
	// TagGroups is used to mark group membership of registry entries
	TagGroups = "groups"

	// KindEntry stores value in registry without propagation, useful for app specific configs.
	KindEntry Kind = "registry.entry"

	// KindNamespaceRequirement represents namespace requirement variable which can be declared by export or any other source.
	KindNamespaceRequirement Kind = "ns.requirement"

	// KindNamespaceDependency represents a module dependency entry
	KindNamespaceDependency Kind = "ns.dependency"
)

type (
	// Kind is a string representing the type of an entry (e.g., "listener", "service", "endpoint")
	Kind = string

	// Namespace represents a unique identifier for a registry namespace
	Namespace = string

	// Name represents a unique identifier for a registry entry within a single namespace
	Name = string

	// Version represents a specific version of the registry's state
	Version interface {
		// ID returns the unique identifier of the version
		ID() uint
		// Previous returns the previous Version, or nil if this is the root version
		Previous() Version
		// Next returns the next Version if available, with a boolean indicating presence
		Next() (Version, bool)
		// String returns a string representation of the version
		String() string
	}

	// State represents the complete state of the registry at a specific version
	State []Entry

	// Entry represents a single entry in the registry
	Entry struct {
		// ID is the unique identifier of the entry
		ID ID `json:"id"`
		// Kind is the type/category of the entry
		Kind Kind `json:"kind"`
		// Meta contains any additional metadata about the entry
		Meta Metadata `json:"meta"`
		// Data is the actual payload associated with the entry
		Data payload.Payload `json:"data"`
	}

	// ChangeSet represents a set of operations to transition the registry from one state to another
	ChangeSet []Operation

	// Operation represents a single operation within a ChangeSet
	Operation struct {
		// Kind is the type of operation (create, update, delete)
		Kind event.Kind `json:"kind"`
		// Entry is the entry affected by the operation
		Entry Entry `json:"entry"`
		// OriginalEntry stores the entry value before the operation for reversal purposes
		// For Update: contains the entry before the update
		// For Delete: contains the deleted entry
		// For Create: nil
		OriginalEntry *Entry `json:"original_entry,omitempty"`
	}

	// DependencyPattern defines a pattern for extracting dependencies from entries
	DependencyPattern struct {
		// Path is the location in entry metadata/data to search (e.g., "meta.server", "data.fs")
		Path string
		// Description explains what this pattern matches
		Description string
		// AllowWildcard enables wildcard matching in the path (e.g., "data.imports.*")
		AllowWildcard bool
	}

	// DependencyResolver extracts dependencies from registry entries
	DependencyResolver interface {
		// Extract returns all dependency IDs from an entry
		Extract(entry Entry) []string
		// RegisterPattern adds a new dependency pattern
		RegisterPattern(pattern DependencyPattern) error
	}

	// Registry is the primary interface for interacting with the registry
	Registry interface {
		EntryReader
		StateWriter
		// Current returns the current version of the registry's state
		Current() (Version, error)
		History() History
		// RegisterDependencyPattern adds a pattern for dependency extraction
		RegisterDependencyPattern(pattern DependencyPattern) error
	}

	// StateWriter defines methods for applying changes to the registry's state
	StateWriter interface {
		// Apply applies a ChangeSet to the registry, creating a new version
		Apply(context.Context, ChangeSet) (Version, error)
		// ApplyVersion applies a specific version to the registry
		ApplyVersion(context.Context, Version) error
		// LoadState initializes registry state from baseline and history without creating new version records
		LoadState(context.Context, State, Version) error
	}

	// StateBuilder defines methods for constructing registry states and calculating differences
	StateBuilder interface {
		// BuildState constructs the complete registry state at a specific version
		BuildState(History, Version) (State, error)
		// BuildDelta calculates the minimal ChangeSet required to transition between states
		BuildDelta(State, State) (ChangeSet, error)
	}

	// EntryReader defines methods for reading entries from the registry
	EntryReader interface {
		// GetAllEntries retrieves all entries in the registry's current state
		GetAllEntries() ([]Entry, error)
		// GetEntry retrieves a specific entry by its ID
		GetEntry(ID) (Entry, error)
	}

	// History defines methods for managing the version history of the registry
	History interface {
		// Versions returns a list of all versions available in the history
		Versions() ([]Version, error)
		// Get retrieves the ChangeSet associated with a specific version
		Get(Version) (ChangeSet, error)
		// Save records a new version and its associated ChangeSet in the history
		Save(v Version, cs ChangeSet, head bool) error
		// Head returns the current head version of the history
		Head() (Version, error)
		// SetHead sets given version as head version
		SetHead(Version) error
	}

	// Runner defines how ChangeSets are applied to a State to produce a new State
	Runner interface {
		// Transition applies a given ChangeSet to a State and returns the resulting modified State
		Transition(context.Context, State, ChangeSet) (State, error)
	}

	// Finder defines methods for searching registry entries based on metadata
	// criteria, allowing for flexible queries against the registry.
	Finder interface {
		// Find retrieves all entries with metadata matching the provided criteria
		// and returns them as a slice of entries.
		Find(meta Metadata) ([]Entry, error)
	}

	// EntryListener is an interface for components that want to listen to changes in the registry
	EntryListener interface {
		// Add is called when a new entry is created
		Add(context.Context, Entry) error
		// Update is called when an existing entry is updated
		Update(context.Context, Entry) error
		// Delete is called when an entry is removed
		Delete(context.Context, Entry) error
	}

	// TransactionListener is an interface for components that want to track transaction lifecycle
	TransactionListener interface {
		// Begin is called when a transaction begins
		Begin(ctx context.Context)
		// Commit is called when a transaction successfully completes
		Commit(ctx context.Context)
		// Discard is called when a transaction is rolled back
		Discard(ctx context.Context)
	}
)
