package __OOOOLD

import (
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
	"sort"
	"strconv"
	"strings"
)

const (
	initialVersion    = "reg:v000.000"
	actionsPerVersion = 1000
)

type history struct {
	entries         []versionedActions                    // History of actions, ordered from oldest to newest
	versions        map[registry.Version][]registry.Entry // State at each version
	versionSequence uint64                                // Sequence number for major version (e.g., the 001 in reg:v001.000)
	actionCounter   uint64                                // Counter for actions within the current major version
}

type versionedActions struct {
	version registry.Version  // Version to which these actions apply
	actions []registry.Action // List of actions
}

// newHistory creates a new history instance.
func newHistory() *history {
	return &history{
		entries:         make([]versionedActions, 0),
		versions:        make(map[registry.Version][]registry.Entry),
		versionSequence: 0,
		actionCounter:   0,
	}
}

// addVersion adds a new version to the history.
func (h *history) addVersion(v registry.Version, actions []registry.Action) error {
	// Defensive copying of actions
	actionsCopy := make([]registry.Action, len(actions))
	copy(actionsCopy, actions)

	h.entries = append(h.entries, versionedActions{
		version: v,
		actions: actionsCopy,
	})

	// Reconstruct the state from actions
	entries := h.reconstructState(v)

	// Save the new version state
	h.versions[v] = entries

	return nil
}

// getVersionState returns the state at a specific version.
func (h *history) getVersionState(v registry.Version) ([]registry.Entry, error) {
	if entries, ok := h.versions[v]; ok {
		entriesCopy := make([]registry.Entry, len(entries))
		copy(entriesCopy, entries)
		return entriesCopy, nil
	}
	return nil, fmt.Errorf("version %s not found in history", v)
}

// reconstructState reconstructs the state of the registry at a given version
func (h *history) reconstructState(targetVersion registry.Version) []registry.Entry {
	// Find the index of the target version
	targetIndex := -1
	for i, va := range h.entries {
		if va.version == targetVersion {
			targetIndex = i
			break
		}
	}

	if targetIndex == -1 {
		return nil // Or handle the error as appropriate
	}

	// Start with an empty state
	state := make(map[registry.Path]*registry.Entry)

	// Iterate through the actions up to the target version
	for _, va := range h.entries[:targetIndex+1] {
		for _, action := range va.actions {
			switch action.Kind {
			case registry.Create:
				state[action.Entry.Path] = &registry.Entry{
					Path: action.Entry.Path,
					Data: action.Entry.Data,
				}
			case registry.Update:
				if entry, ok := state[action.Entry.Path]; ok {
					entry.Data = action.Entry.Data
					// Update other fields if necessary
				}
			case registry.Delete:
				delete(state, action.Entry.Path)
			}
		}
	}

	// Convert the map to a slice and sort it
	entries := make([]registry.Entry, 0, len(state))
	for _, entry := range state {
		entries = append(entries, *entry)
	}
	sortEntries(entries)

	return entries
}

// rebuild reconstructs the history from the current version back.
func (h *history) rebuild(currentVersion registry.Version, currentEntries map[registry.Path]*registry.Entry) {
	h.entries = h.entries[:0] // Clear history

	// Save the current state as the first entry in the rebuilt history
	currentVersionEntries := make([]registry.Entry, 0, len(currentEntries))
	for _, entry := range currentEntries {
		currentVersionEntries = append(currentVersionEntries, *entry)
	}
	sortEntries(currentVersionEntries)
	h.versions[currentVersion] = currentVersionEntries

	for {
		if currentVersion == initialVersion {
			break
		}

		// Find the previous version by decrementing the counter or sequence
		var prevVersion registry.Version
		if h.actionCounter > 0 {
			prevVersion = registry.Version(fmt.Sprintf("reg:v%03d.%03d", h.versionSequence, h.actionCounter-1))
		} else {
			if h.versionSequence == 0 {
				break // Should not happen, but handle for safety
			}
			prevVersion = registry.Version(fmt.Sprintf("reg:v%03d.%03d", h.versionSequence-1, actionsPerVersion-1))
		}

		// Try to get the previous version from the versions map
		if prevEntries, ok := h.versions[prevVersion]; ok {
			// Calculate the actions that led to the current version
			actions := diffEntries(prevEntries, currentVersionEntries)

			h.entries = append([]versionedActions{{
				version: currentVersion,
				actions: actions,
			}}, h.entries...)

			currentVersion = prevVersion
			currentVersionEntries = prevEntries
		} else {
			// Previous version not found in memory, stop rebuilding
			break
		}

		// Update sequence and counter for the next iteration
		parts := strings.Split(string(currentVersion), ".")
		if len(parts) != 2 {
			break // Should not happen, invalid version format
		}
		vSeq, err := strconv.ParseUint(parts[0][5:], 10, 64)
		if err != nil {
			break // Handle strconv error (unlikely)
		}
		aCount, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			break // Handle strconv error (unlikely)
		}

		h.versionSequence = vSeq
		h.actionCounter = aCount
	}
}

// updateVersion updates the version and action counter.
func (h *history) updateVersion(actionCount uint64) registry.Version {
	h.actionCounter += actionCount
	if h.actionCounter >= actionsPerVersion {
		h.versionSequence++
		h.actionCounter = 0
	}
	return registry.Version(fmt.Sprintf("reg:v%03d.%03d", h.versionSequence, h.actionCounter))
}

// getVersionSequenceAndActionCounter returns the version sequence and action counter.
func (h *history) getVersionSequenceAndActionCounter() (uint64, uint64) {
	return h.versionSequence, h.actionCounter
}

// setVersionSequenceAndActionCounter sets the version sequence and action counter.
func (h *history) setVersionSequenceAndActionCounter(versionSequence, actionCounter uint64) error {
	if actionCounter >= actionsPerVersion {
		return fmt.Errorf("actionCounter must be less than %d", actionsPerVersion)
	}
	h.versionSequence = versionSequence
	h.actionCounter = actionCounter
	return nil
}

func sortEntries(entries []registry.Entry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})
}
