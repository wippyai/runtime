// SPDX-License-Identifier: MPL-2.0

// Package registry provides a versioned storage system for configuration entries.
package registry

import (
	"context"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
)

// Registry system constants define the various event types and identifiers used throughout the registry.
const (
	// System identifies the registry system in the event bus.
	System event.System = "registry"

	// EntryCreate represents an event for creating a new registry entry.
	EntryCreate event.Kind = "entry.create"
	// EntryUpdate represents an event for updating an existing registry entry.
	EntryUpdate event.Kind = "entry.update"
	// EntryDelete represents an event for removing a registry entry.
	EntryDelete event.Kind = "entry.delete"

	// EntryAccept represents an event for accepting a registry entry.
	EntryAccept event.Kind = "entry.accept"
	// EntryReject represents an event for rejecting a registry entry.
	EntryReject event.Kind = "entry.reject"
	// EntryResult matches entry accept/reject replies.
	EntryResult event.Kind = "entry.(accept|reject)"

	// TxBegin represents the start of a registry transaction.
	TxBegin event.Kind = "registry.begin"
	// TxCommit represents the successful completion of a registry transaction.
	TxCommit event.Kind = "registry.commit"
	// TxDiscard represents the rollback of a registry transaction.
	TxDiscard event.Kind = "registry.discard"
	// TxAccept confirms a transaction lifecycle event has been handled.
	TxAccept event.Kind = "registry.accept"
	// TxReject rejects a transaction lifecycle event.
	TxReject event.Kind = "registry.reject"
	// TxResult matches transaction lifecycle accept/reject replies.
	TxResult event.Kind = "registry.(accept|reject)"

	// AllEvents matches any registry operation event.
	AllEvents event.Kind = "(entry|registry).(create|update|delete|begin|commit|discard)"

	// RootVersion represents the initial version of the registry.
	RootVersion uint = 0

	// TagDependsOn is used to mark dependencies between registry entries, groups and ns.
	TagDependsOn = "depends_on"
	// TagGroups is used to mark group membership of registry entries.
	TagGroups = "groups"

	// EntryKind stores value in registry without propagation, useful for app specific configs.
	EntryKind Kind = "registry.entry"

	// NamespaceRequirement represents namespace requirement variable which can be declared by export or any other source.
	NamespaceRequirement Kind = "ns.requirement"

	// NamespaceDependency represents a module dependency entry
	NamespaceDependency Kind = "ns.dependency"

	// NamespaceDefinition represents module metadata (readme, license, authors, etc.)
	NamespaceDefinition Kind = "ns.definition"
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
		// Next returns the next Version, or nil if this is the current version
		Next() Version
		// String returns a string representation of the version
		String() string
	}

	// State represents the complete state of the registry at a specific version
	State []Entry

	// Entry represents a single entry in the registry
	Entry struct {
		Data payload.Payload `json:"data"`
		Meta attrs.Bag       `json:"meta"`
		ID   ID              `json:"id"`
		Kind Kind            `json:"kind"`
	}

	// ChangeSet represents a set of operations to transition the registry from one state to another
	ChangeSet []Operation

	// Operation represents a single operation within a ChangeSet
	Operation struct {
		Entry         Entry      `json:"entry"`
		OriginalEntry *Entry     `json:"original_entry,omitempty"`
		Kind          event.Kind `json:"kind"`
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

	// StateMap is a map-based representation of registry state for efficient lookups
	StateMap map[ID]Entry

	// StateBuilder defines methods for constructing registry states and calculating differences
	StateBuilder interface {
		// BuildState constructs the complete registry state at a specific version
		BuildState(History, Version) (State, error)
		// BuildDelta calculates the minimal ChangeSet required to transition between states
		BuildDelta(State, State) (ChangeSet, error)
		// SquashChangesets aggregates multiple changesets into a single changeset
		SquashChangesets([]ChangeSet) ChangeSet
		// ReverseChangeset creates a changeset that undoes the given changeset operations
		ReverseChangeset(ChangeSet) (ChangeSet, error)
		// SortChangeSet orders a ChangeSet for safe application: deletes first in
		// reverse-dependency order (dependants before dependees), then creates and
		// updates in forward-dependency order. fromState is the state the deletes
		// apply against, used to resolve current dependency edges.
		SortChangeSet(fromState State, cs ChangeSet) (ChangeSet, error)
	}

	// OperationHandler defines methods for validating and applying registry operations
	OperationHandler interface {
		// ValidateOperation checks if an operation is valid for the given state
		ValidateOperation(StateMap, Operation) error
		// ApplyOperation applies an operation to the state and returns the new state
		ApplyOperation(StateMap, Operation) (StateMap, error)
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
		Find(meta attrs.Bag) ([]Entry, error)
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
		Begin(ctx context.Context) error
		// Commit is called when a transaction successfully completes
		Commit(ctx context.Context) error
		// Discard is called when a transaction is rolled back
		Discard(ctx context.Context) error
	}

	// TransactionParticipant marks event handlers that acknowledge registry transaction events.
	TransactionParticipant interface {
		// RegistryTransactionParticipantID returns the participant's stable in-process reply id.
		RegistryTransactionParticipantID() string
	}
)
