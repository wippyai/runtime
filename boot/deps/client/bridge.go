package client

import (
	"context"
	"slices"

	"github.com/Masterminds/semver/v3"
	modulev1 "github.com/wippyai/module-registry-proto-go/registry/module/v1"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/loader"
	"github.com/wippyai/runtime/boot/loader/interpolate"
	lru "github.com/wippyai/runtime/internal/cache"
	"go.uber.org/zap"
)

// ManifestBridge implements graph.ManifestProvider using the registry API.
// HACK: This is a temporary bridge until a proper manifest API exists.
// It downloads modules, filters YAML files, uses boot/loader to parse them,
// and extracts dependencies using YAML parsing logic.
type ManifestBridge struct {
	client *RegistryClient
	dtt    payload.Transcoder
	loader *loader.Loader
	cache  *lru.Cache[string, *graph.Manifest]
	log    *zap.Logger
}

// NewManifestBridge creates a new manifest bridge with LRU cache.
// Cache size is optimized for lock file recalculation scenarios.
func NewManifestBridge(
	client *RegistryClient,
	dtt payload.Transcoder,
	log *zap.Logger,
	cacheSize int,
) (*ManifestBridge, error) {
	if cacheSize <= 0 {
		cacheSize = 100
	}

	manifestCache := lru.New[string, *graph.Manifest](lru.WithCapacity(cacheSize))

	if log == nil {
		log = zap.NewNop()
	}

	interpolator := interpolate.NewEntryInterpolator(dtt)
	loaderInstance := loader.NewLoader(dtt, log, interpolator)

	return &ManifestBridge{
		client: client,
		dtt:    dtt,
		loader: loaderInstance,
		cache:  manifestCache,
		log:    log,
	}, nil
}

// FetchManifests implements graph.ManifestProvider.
func (b *ManifestBridge) FetchManifests(ctx context.Context, requests []graph.ManifestRequest) ([]graph.ManifestResponse, error) {
	if len(requests) == 0 {
		return nil, nil
	}

	orgNames := make([]string, 0, len(requests))
	seen := make(map[string]struct{})
	for _, req := range requests {
		if _, ok := seen[req.Name.Organization]; ok {
			continue
		}
		orgNames = append(orgNames, req.Name.Organization)
		seen[req.Name.Organization] = struct{}{}
	}

	orgs, err := b.client.GetOrganizations(ctx, orgNames)
	if err != nil {
		return nil, NewGetOrganizationsError(err)
	}

	orgMap := make(map[string]string)
	for _, org := range orgs {
		orgMap[org.Name] = org.Organization.GetId()
	}

	moduleRequests := make([]ModuleInfo, 0, len(requests))
	for _, req := range requests {
		moduleRequests = append(moduleRequests, ModuleInfo{
			OrganizationID: orgMap[req.Name.Organization],
			Name:           req.Name.Module,
		})
	}

	modules, err := b.client.GetModules(ctx, moduleRequests)
	if err != nil {
		return nil, NewGetModulesError(err)
	}

	moduleIDs := make([]string, len(modules))
	for i, mod := range modules {
		moduleIDs[i] = mod.Module.GetId()
	}

	labels, err := b.client.GetLabels(ctx, moduleIDs)
	if err != nil {
		return nil, NewGetLabelsError(err)
	}

	responses := make([]graph.ManifestResponse, len(requests))
	for i, req := range requests {
		responses[i] = b.processRequest(ctx, req, i, orgs, modules, labels)
	}

	return responses, nil
}

func (b *ManifestBridge) processRequest(
	ctx context.Context,
	req graph.ManifestRequest,
	index int,
	orgs []OrganizationInfo,
	modules []ModuleInfo,
	labels []LabelInfo,
) graph.ManifestResponse {
	constraint, err := semver.NewConstraint(req.Constraint)
	if err != nil {
		return graph.ManifestResponse{
			Request: req,
			Error:   NewParseConstraintError(req.Constraint, err),
		}
	}

	matchingLabel, err := b.findMatchingLabel(labels[index].Labels, constraint)
	if err != nil {
		return graph.ManifestResponse{
			Request: req,
			Error:   err,
		}
	}

	cacheKey := req.Name.String() + "@" + matchingLabel.GetCommitId()
	if manifest, ok := b.cache.Get(cacheKey); ok {
		return graph.ManifestResponse{
			Request:       req,
			Organization:  orgs[index].Organization,
			Module:        modules[index].Module,
			Labels:        labels[index].Labels,
			SelectedLabel: matchingLabel,
			Manifest:      manifest,
		}
	}

	manifest, err := b.fetchManifest(ctx, matchingLabel.GetCommitId())
	if err != nil {
		return graph.ManifestResponse{
			Request: req,
			Error:   NewFetchManifestError(err),
		}
	}

	_ = b.cache.Set(cacheKey, manifest)

	return graph.ManifestResponse{
		Request:       req,
		Organization:  orgs[index].Organization,
		Module:        modules[index].Module,
		Labels:        labels[index].Labels,
		SelectedLabel: matchingLabel,
		Manifest:      manifest,
	}
}

func (b *ManifestBridge) findMatchingLabel(allLabels []*modulev1.Label, constraint *semver.Constraints) (*modulev1.Label, error) {
	var matchingLabels []*modulev1.Label

	for _, label := range allLabels {
		version := label.GetName()
		v, err := semver.NewVersion(version)
		if err != nil {
			b.log.Debug("skip invalid version", zap.String("version", version))
			continue
		}

		if constraint.Check(v) {
			matchingLabels = append(matchingLabels, label)
		}
	}

	if len(matchingLabels) == 0 {
		return nil, ErrNoMatchingVersion
	}

	slices.SortFunc(matchingLabels, func(a, b *modulev1.Label) int {
		aVer, _ := semver.NewVersion(a.GetName())
		bVer, _ := semver.NewVersion(b.GetName())
		return bVer.Compare(aVer)
	})

	return matchingLabels[0], nil
}

func (b *ManifestBridge) fetchManifest(ctx context.Context, commitID string) (*graph.Manifest, error) {
	downloads, err := b.client.Download(ctx, []string{commitID})
	if err != nil {
		return nil, NewDownloadCommitError(commitID, err)
	}

	if len(downloads) == 0 {
		return nil, ErrNoContentDownloaded
	}

	memfs, err := NewMemFS(downloads[0].Files)
	if err != nil {
		return nil, NewCreateInMemoryFSError(err)
	}

	entries, err := b.loader.LoadFS(ctx, memfs)
	if err != nil {
		return nil, NewLoadEntriesFromFSError(err)
	}

	deps, err := extractDependenciesFromEntries(entries, b.dtt)
	if err != nil {
		return nil, NewExtractDependenciesError(err)
	}

	if len(deps) == 0 {
		deps = []graph.ManifestDependency{}
	}

	return &graph.Manifest{
		Name:         "",
		Version:      "",
		Dependencies: deps,
	}, nil
}
