package graph

import (
	"context"

	"github.com/wippyai/runtime/internal/graph"
)

// buildGraph performs the actual graph building.
func (b *Builder) buildGraph(ctx context.Context, input BuildInput) (*BuildResult, error) {
	g := graph.New[ModuleKey, DependencyEdge]()
	resolved := make(map[ModuleKey]ResolvedModule)
	processed := make(map[Name]ModuleKey) // one version per module name
	detector := newOverlapDetector()
	stats := BuildStats{}

	// Initialize with root dependencies
	currentLevel := make([]pendingModule, 0, len(input.RootDependencies))

	for _, req := range input.RootDependencies {
		currentLevel = append(currentLevel, pendingModule{
			Request: req,
			Level:   0,
			Parent:  nil,
		})
		// Record constraint with empty parent (root)
		detector.recordRequest(req.Name, req.Constraint, ModuleKey{})
	}

	level := 0
	for len(currentLevel) > 0 {
		// Build manifest requests for this level
		manifestReqs := make([]ManifestRequest, len(currentLevel))
		for i, pm := range currentLevel {
			manifestReqs[i] = ManifestRequest{
				Name:       pm.Request.Name,
				Constraint: pm.Request.Constraint,
			}
		}

		// Batch fetch manifests
		manifests, err := b.provider.FetchManifests(ctx, manifestReqs)
		if err != nil {
			return nil, NewFetchManifestsError(level, err)
		}

		stats.ManifestsFetched += len(manifests)

		// Process responses
		nextLevel := make([]pendingModule, 0)

		for i, manifest := range manifests {
			pm := currentLevel[i]

			// Check for fetch error
			if manifest.Error != nil {
				return nil, NewFetchManifestError(pm.Request.Name, manifest.Error)
			}

			// Resolve version
			selectedLabel := manifest.SelectedLabel
			if selectedLabel == nil {
				return nil, NewNoMatchingVersionForModuleError(pm.Request.Name, pm.Request.Constraint)
			}

			moduleKey := ModuleKey{
				Name:    pm.Request.Name,
				Version: selectedLabel.GetName(),
			}

			// Check if already processed
			if existing, exists := processed[pm.Request.Name]; exists {
				// Already have this module
				if existing.Version != moduleKey.Version {
					// Different version resolved - this is a conflict
					// Will be detected in conflict detection phase
					continue
				}

				// Same version - just add edge if has parent
				if pm.Parent != nil {
					g.AddEdge(*pm.Parent, moduleKey, 1, DependencyEdge{
						Constraint: pm.Request.Constraint,
						DeclaredBy: *pm.Parent,
						IsDirect:   false,
					})
				}
				continue
			}

			// New module - add to graph
			g.AddNode(moduleKey)

			resolved[moduleKey] = ResolvedModule{
				Name:         pm.Request.Name,
				Version:      moduleKey.Version,
				CommitID:     selectedLabel.GetCommitId(),
				Labels:       manifest.Labels,
				Constraint:   pm.Request.Constraint,
				Organization: manifest.Organization,
				Module:       manifest.Module,
			}

			processed[pm.Request.Name] = moduleKey
			stats.TotalModules++

			// Add edge
			isDirect := pm.Parent == nil
			if pm.Parent != nil {
				g.AddEdge(*pm.Parent, moduleKey, 1, DependencyEdge{
					Constraint: pm.Request.Constraint,
					DeclaredBy: *pm.Parent,
					IsDirect:   false,
				})
			}

			// Process manifest dependencies
			if manifest.Manifest != nil {
				for _, dep := range manifest.Manifest.Dependencies {
					// Skip local dependencies
					if dep.Path != "" {
						continue
					}

					nextLevel = append(nextLevel, pendingModule{
						Request: DependencyRequest{
							Name:        dep.Name,
							Constraint:  dep.Version,
							RequestedBy: moduleKey.String(),
						},
						Level:  level + 1,
						Parent: &moduleKey,
					})

					// Record constraint
					detector.recordRequest(dep.Name, dep.Version, moduleKey)
				}
			}

			// Mark as direct dependency if root
			if isDirect {
				// Update resolved module to mark as direct
				rm := resolved[moduleKey]
				resolved[moduleKey] = rm
			}
		}

		currentLevel = nextLevel
		level++
	}

	stats.TotalLevels = level

	// Detect overlaps and conflicts
	overlaps := detector.detectOverlaps()
	conflicts := detectConflicts(overlaps, resolved, g)
	stats.ConflictsFound = len(conflicts)

	return &BuildResult{
		Graph:           g,
		ResolvedModules: resolved,
		Conflicts:       conflicts,
		Stats:           stats,
	}, nil
}
