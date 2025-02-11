package registry

import (
	"context"
	"encoding/json"
	"fmt"
	contextapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/events"
	"github.com/ponyruntime/pony/api/payload"
	"strings"
)

// Registry system constants define the various event types and identifiers used throughout the registry.
// These constants are used to identify different operations and states in the registry system.
const (
	// System identifies the registry system in the event context
	System events.System = "registry"

	// Create represents an event for creating a new registry entry
	Create events.Kind = "entry.create"
	// Update represents an event for updating an existing registry entry
	Update events.Kind = "entry.update"
	// Delete represents an event for removing a registry entry
	Delete events.Kind = "entry.delete"
	// Accept represents an event for accepting a registry entry
	Accept events.Kind = "entry.accept"
	// Reject represents an event for rejecting a registry entry
	Reject events.Kind = "entry.reject"

	// Changes represents a pattern matching any create, update, or delete events
	Changes events.Kind = "entry.(create|update|delete)"

	// Begin represents the start of a registry transaction
	Begin events.Kind = "registry.begin"
	// Commit represents the successful completion of a registry transaction
	Commit events.Kind = "registry.commit"
	// Discard represents the rollback of a registry transaction
	Discard events.Kind = "registry.discard"

	// RootVersion represents the initial version of the registry
	RootVersion uint = 0

	// TagDependsOn is used to mark dependencies between registry entries, groups and ns.
	TagDependsOn = "depends_on"

	// TagGroups is used to mark group membership of registry entries.
	TagGroups = "groups"
)

type (
	// Kind is a string representing the type of an entry (e.g., "listener", "service", "endpoint").
	// This helps categorize entries for different purposes.
	Kind = string

	// Namespace represents a unique identifier for a registry namespace bounding components within a specific context.
	Namespace = string

	// Name represents a unique identifier for a registry entry within a single namespace. Most entity events are identified by this Name.
	// It typically uses a hierarchical structure (e.g., "service.database.url").
	Name = string

	ID struct {
		// Namespace is the namespace of the target.
		NS Namespace `json:"ns"`
		// Name is the unique (within ns) identifier of the target.
		Name Name `json:"name"`
	}

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
		// ID is the unique identifier of the entry.
		ID ID `json:"id"`
		// Kind is the type/category of the entry.
		Kind Kind `json:"kind"`
		// Meta contains any additional metadata about the entry.
		Meta Metadata `json:"meta"`
		// Data is the actual payload associated with the entry, providing its value or configuration.
		Data payload.Payload `json:"data"`
	}

	// ChangeSet represents a set of operations that, when applied sequentially, transition the registry from one state to another.
	ChangeSet []Operation

	// Operation represents a single operation within a ChangeSet (e.g., create, update, or delete an entry).
	Operation struct {
		// Kind is the type of operation.
		Kind events.Kind `json:"kind"`
		// Entry is the entry affected by the operation. For Delete operations, only the Name field might be relevant.
		Entry Entry `json:"entry"`
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
		GetEntry(ID) (Entry, error)
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

	// EntryListener is an interface for components that want to listen to changes in the registry.
	EntryListener interface {
		Add(context.Context, Entry) error
		Update(context.Context, Entry) error
		Delete(context.Context, Entry) error
	}
)

func GetRegistry(ctx context.Context) Registry {
	return ctx.Value(contextapi.RegistryCtx).(Registry)
}

func (t ID) String() string {
	return fmt.Sprintf("%s:%s", t.NS, t.Name)
}

// WithDefaultNS returns a new ID with the given default namespace if one is not already set.
// If the ID already has a namespace, it returns the ID unchanged.
func (t ID) WithDefaultNS(defaultNS Namespace) ID {
	if t.NS != "" {
		return t
	}
	return ID{
		NS:   defaultNS,
		Name: t.Name,
	}
}

func (t *ID) UnmarshalJSON(data []byte) error {
	// Check if the data is a JSON string
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}

		// Handle string format - split on first colon
		parts := strings.SplitN(s, ":", 2)
		if len(parts) == 1 {
			// Alias-only format
			t.NS = ""
			t.Name = Name(parts[0])
			return nil
		}
		// Has colon - parse as ns:name
		t.NS = Namespace(parts[0])
		t.Name = Name(parts[1])
		return nil
	}

	// Handle object format
	var obj struct {
		NS Namespace `json:"ns"`
		ID Name      `json:"id"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	if obj.ID == "" {
		return fmt.Errorf("missing required field 'id'")
	}
	t.NS = obj.NS
	t.Name = obj.ID
	return nil
}

// ParseID creates an ID from a string in either "namespace:name" or "name-only" format.
// For "namespace:name" format, the first colon is used as the separator.
// For "name-only" format, an empty namespace is used.
func ParseID(s string) ID {
	// Split on first colon
	parts := strings.SplitN(s, ":", 2)
	if len(parts) == 1 {
		// Alias-only format
		return ID{
			NS:   "",
			Name: Name(parts[0]),
		}
	}
	// Has colon - parse as ns:name
	return ID{
		NS:   Namespace(parts[0]),
		Name: Name(parts[1]),
	}
}
