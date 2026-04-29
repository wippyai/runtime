// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"connectrpc.com/connect"
	lua "github.com/wippyai/go-lua"
	modulev1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/module/v1"
	modulev1connect "github.com/wippyai/runtime/api/hub/wippy/api/hub/module/v1/modulev1connect"
	versionv1 "github.com/wippyai/runtime/api/hub/wippy/api/hub/version/v1"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	bootauth "github.com/wippyai/runtime/boot/deps/auth"
	boothub "github.com/wippyai/runtime/boot/deps/hub"
	"github.com/wippyai/runtime/runtime/security"
)

// Options configure the hub module.
type Options struct {
	ModuleClient   modulev1connect.ModuleServiceClient
	ArtifactClient ArtifactClient
	AuthStore      *bootauth.Store
	BaseURL        string
	Token          string
	Timeout        time.Duration
}

// DefaultOptions returns the default module options.
func DefaultOptions() Options {
	return Options{}
}

// Module is the default hub module with default options.
var Module = NewModule(DefaultOptions())

type hubModule struct {
	store *bootauth.Store
	opts  Options
}

// NewModule creates a hub module with the given options.
func NewModule(opts Options) *luaapi.ModuleDef {
	h := newHubModule(opts)

	return &luaapi.ModuleDef{
		Name:        "hub",
		Description: "Hub module registry browsing and metadata",
		Class:       []string{luaapi.ClassNetwork, luaapi.ClassIO, luaapi.ClassNondeterministic},
		Build:       h.build,
		Types:       ModuleTypes,
	}
}

func newHubModule(opts Options) *hubModule {
	return &hubModule{opts: opts}
}

func (h *hubModule) authStore() *bootauth.Store {
	if h == nil {
		return nil
	}
	if h.store != nil {
		return h.store
	}
	if h.opts.AuthStore != nil {
		h.store = h.opts.AuthStore
		return h.store
	}

	projectDir := ""
	if wd, err := os.Getwd(); err == nil {
		projectDir = wd
	}
	h.store = bootauth.NewStore(bootauth.NewConfig(projectDir))
	return h.store
}

func (h *hubModule) build() (*lua.LTable, []luaapi.YieldType) {
	mod := lua.CreateTable(0, 5)

	modules := lua.CreateTable(0, 4)
	modules.RawSetString("list", lua.LGoFunc(h.modulesList))
	modules.RawSetString("search", lua.LGoFunc(h.modulesSearch))
	modules.RawSetString("get", lua.LGoFunc(h.modulesGet))
	modules.RawSetString("readme", lua.LGoFunc(h.modulesReadme))
	modules.Immutable = true

	versions := lua.CreateTable(0, 3)
	versions.RawSetString("list", lua.LGoFunc(h.versionsList))
	versions.RawSetString("get", lua.LGoFunc(h.versionsGet))
	versions.RawSetString("inspect", lua.LGoFunc(h.versionsInspect))
	versions.Immutable = true

	dependencies := lua.CreateTable(0, 1)
	dependencies.RawSetString("get", lua.LGoFunc(h.dependenciesGet))
	dependencies.Immutable = true

	dependents := lua.CreateTable(0, 1)
	dependents.RawSetString("get", lua.LGoFunc(h.dependentsGet))
	dependents.Immutable = true

	files := lua.CreateTable(0, 1)
	files.RawSetString("list", lua.LGoFunc(h.filesList))
	files.Immutable = true

	mod.RawSetString("modules", modules)
	mod.RawSetString("versions", versions)
	mod.RawSetString("dependencies", dependencies)
	mod.RawSetString("dependents", dependents)
	mod.RawSetString("files", files)
	mod.Immutable = true

	return mod, nil
}

// Module API

func (h *hubModule) modulesList(l *lua.LState) int {
	ctx, err := h.requireContext(l)
	if err != nil {
		return pushError(l, err)
	}
	if !security.IsAllowed(ctx, "hub.modules.list", "", nil) {
		return pushError(l, permissionDenied(l, "hub.modules.list", ""))
	}

	base, opts, err := parseListModulesOptions(l, 1)
	if err != nil {
		return pushError(l, err)
	}

	client, err := h.moduleClient(l, base)
	if err != nil {
		return pushError(l, err)
	}

	ctx, cancel := withTimeout(ctx, base.timeout)
	defer cancel()

	req := &modulev1.ListModulesRequest{
		OrganizationId: opts.organizationID,
		Page:           opts.page,
		PageSize:       opts.pageSize,
		Visibility:     opts.visibility,
		Type:           opts.moduleType,
		SortOrder:      opts.sortOrder,
	}

	resp, callErr := client.ListModules(ctx, connect.NewRequest(req))
	if callErr != nil {
		return pushError(l, hubCallError(l, callErr))
	}

	return pushListResponse(l, resp.Msg.Modules, resp.Msg.Total, resp.Msg.Page, resp.Msg.PageSize)
}

func (h *hubModule) modulesSearch(l *lua.LState) int {
	query := l.OptString(1, "")
	if strings.TrimSpace(query) == "" {
		return pushError(l, invalidArgument(l, "query required"))
	}

	ctx, err := h.requireContext(l)
	if err != nil {
		return pushError(l, err)
	}
	if !security.IsAllowed(ctx, "hub.modules.search", query, nil) {
		return pushError(l, permissionDenied(l, "hub.modules.search", query))
	}

	base, opts, err := parseSearchModulesOptions(l, 2)
	if err != nil {
		return pushError(l, err)
	}

	client, err := h.moduleClient(l, base)
	if err != nil {
		return pushError(l, err)
	}

	ctx, cancel := withTimeout(ctx, base.timeout)
	defer cancel()

	req := &modulev1.SearchModulesRequest{
		Query:             query,
		Page:              opts.page,
		PageSize:          opts.pageSize,
		Keywords:          opts.keywords,
		License:           opts.license,
		IncludeDeprecated: opts.includeDeprecated,
	}

	resp, callErr := client.SearchModules(ctx, connect.NewRequest(req))
	if callErr != nil {
		return pushError(l, hubCallError(l, callErr))
	}

	return pushListResponse(l, resp.Msg.Modules, resp.Msg.Total, resp.Msg.Page, resp.Msg.PageSize)
}

func (h *hubModule) modulesGet(l *lua.LState) int {
	moduleRef, moduleKey, err := parseModuleRef(l, 1)
	if err != nil {
		return pushError(l, err)
	}

	ctx, err := h.requireContext(l)
	if err != nil {
		return pushError(l, err)
	}
	if !security.IsAllowed(ctx, "hub.modules.get", moduleKey, nil) {
		return pushError(l, permissionDenied(l, "hub.modules.get", moduleKey))
	}

	base, err := parseBaseOptions(l, 2)
	if err != nil {
		return pushError(l, err)
	}

	client, err := h.moduleClient(l, base)
	if err != nil {
		return pushError(l, err)
	}

	ctx, cancel := withTimeout(ctx, base.timeout)
	defer cancel()

	req := &modulev1.GetModuleRequest{Module: moduleRef}
	resp, callErr := client.GetModule(ctx, connect.NewRequest(req))
	if callErr != nil {
		return pushError(l, hubCallError(l, callErr))
	}

	l.Push(moduleToTable(l, resp.Msg.Module))
	return 1
}

func (h *hubModule) modulesReadme(l *lua.LState) int {
	moduleRef, moduleKey, err := parseModuleRef(l, 1)
	if err != nil {
		return pushError(l, err)
	}

	ctx, err := h.requireContext(l)
	if err != nil {
		return pushError(l, err)
	}
	if !security.IsAllowed(ctx, "hub.modules.readme", moduleKey, nil) {
		return pushError(l, permissionDenied(l, "hub.modules.readme", moduleKey))
	}

	base, opts, err := parseReadmeOptions(l, 2)
	if err != nil {
		return pushError(l, err)
	}

	client, err := h.moduleClient(l, base)
	if err != nil {
		return pushError(l, err)
	}

	ctx, cancel := withTimeout(ctx, base.timeout)
	defer cancel()

	req := &modulev1.GetReadmeRequest{Module: moduleRef, Version: opts.version}
	resp, callErr := client.GetReadme(ctx, connect.NewRequest(req))
	if callErr != nil {
		return pushError(l, hubCallError(l, callErr))
	}

	result := lua.CreateTable(0, 3)
	result.RawSetString("content", lua.LString(resp.Msg.GetContent()))
	result.RawSetString("filename", lua.LString(resp.Msg.GetFilename()))
	result.RawSetString("version", lua.LString(resp.Msg.GetVersion()))
	l.Push(result)
	return 1
}

func (h *hubModule) versionsList(l *lua.LState) int {
	moduleRef, moduleKey, err := parseModuleRef(l, 1)
	if err != nil {
		return pushError(l, err)
	}

	ctx, err := h.requireContext(l)
	if err != nil {
		return pushError(l, err)
	}
	if !security.IsAllowed(ctx, "hub.versions.list", moduleKey, nil) {
		return pushError(l, permissionDenied(l, "hub.versions.list", moduleKey))
	}

	base, opts, err := parseListVersionsOptions(l, 2)
	if err != nil {
		return pushError(l, err)
	}

	client, err := h.moduleClient(l, base)
	if err != nil {
		return pushError(l, err)
	}

	ctx, cancel := withTimeout(ctx, base.timeout)
	defer cancel()

	req := &modulev1.ListVersionsRequest{
		Module:        moduleRef,
		Page:          opts.page,
		PageSize:      opts.pageSize,
		IncludeYanked: opts.includeYanked,
	}

	resp, callErr := client.ListVersions(ctx, connect.NewRequest(req))
	if callErr != nil {
		return pushError(l, hubCallError(l, callErr))
	}

	return pushVersionListResponse(l, resp.Msg.Versions, resp.Msg.Total, resp.Msg.Page, resp.Msg.PageSize)
}

func (h *hubModule) versionsGet(l *lua.LState) int {
	moduleRef, moduleKey, err := parseModuleRef(l, 1)
	if err != nil {
		return pushError(l, err)
	}
	versionRef, err := parseVersionRef(l, 2)
	if err != nil {
		return pushError(l, err)
	}

	ctx, err := h.requireContext(l)
	if err != nil {
		return pushError(l, err)
	}
	if !security.IsAllowed(ctx, "hub.versions.get", moduleKey, nil) {
		return pushError(l, permissionDenied(l, "hub.versions.get", moduleKey))
	}

	base, err := parseBaseOptions(l, 3)
	if err != nil {
		return pushError(l, err)
	}

	client, err := h.moduleClient(l, base)
	if err != nil {
		return pushError(l, err)
	}

	ctx, cancel := withTimeout(ctx, base.timeout)
	defer cancel()

	req := &modulev1.GetVersionRequest{Module: moduleRef, Version: versionRef}
	resp, callErr := client.GetVersion(ctx, connect.NewRequest(req))
	if callErr != nil {
		return pushError(l, hubCallError(l, callErr))
	}

	l.Push(versionToTable(l, resp.Msg.Version))
	return 1
}

func (h *hubModule) versionsInspect(l *lua.LState) int {
	moduleRef, moduleKey, err := parseModuleRef(l, 1)
	if err != nil {
		return pushError(l, err)
	}
	versionRef, err := parseVersionRef(l, 2)
	if err != nil {
		return pushError(l, err)
	}

	ctx, err := h.requireContext(l)
	if err != nil {
		return pushError(l, err)
	}
	if !security.IsAllowed(ctx, "hub.versions.inspect", moduleKey, nil) {
		return pushError(l, permissionDenied(l, "hub.versions.inspect", moduleKey))
	}

	base, err := parseBaseOptions(l, 3)
	if err != nil {
		return pushError(l, err)
	}

	client, err := h.artifactClient(l, base)
	if err != nil {
		return pushError(l, err)
	}
	params, paramsErr := downloadParamsFromRefs(moduleRef, versionRef)
	if paramsErr != nil {
		return pushError(l, invalidArgument(l, paramsErr.Error()))
	}

	ctx, cancel := withTimeout(ctx, base.timeout)
	defer cancel()

	inspection, callErr := inspectVersionArtifact(ctx, client, params, "")
	if callErr != nil {
		return pushError(l, hubCallError(l, callErr))
	}

	l.Push(artifactInspectionToTable(l, inspection))
	return 1
}

func (h *hubModule) dependenciesGet(l *lua.LState) int {
	moduleRef, moduleKey, err := parseModuleRef(l, 1)
	if err != nil {
		return pushError(l, err)
	}
	versionRef, err := parseOptionalVersionRef(l, 2)
	if err != nil {
		return pushError(l, err)
	}

	ctx, err := h.requireContext(l)
	if err != nil {
		return pushError(l, err)
	}
	if !security.IsAllowed(ctx, "hub.dependencies.get", moduleKey, nil) {
		return pushError(l, permissionDenied(l, "hub.dependencies.get", moduleKey))
	}

	base, err := parseBaseOptions(l, 3)
	if err != nil {
		return pushError(l, err)
	}

	client, err := h.moduleClient(l, base)
	if err != nil {
		return pushError(l, err)
	}

	ctx, cancel := withTimeout(ctx, base.timeout)
	defer cancel()

	req := &modulev1.GetDependenciesRequest{Module: moduleRef, Version: versionRef}
	resp, callErr := client.GetDependencies(ctx, connect.NewRequest(req))
	if callErr != nil {
		return pushError(l, hubCallError(l, callErr))
	}

	items := lua.CreateTable(len(resp.Msg.Dependencies), 0)
	for i, dep := range resp.Msg.Dependencies {
		items.RawSetInt(i+1, dependencyToTable(l, dep))
	}
	result := lua.CreateTable(0, 1)
	result.RawSetString("items", items)
	l.Push(result)
	return 1
}

func (h *hubModule) dependentsGet(l *lua.LState) int {
	moduleRef, moduleKey, err := parseModuleRef(l, 1)
	if err != nil {
		return pushError(l, err)
	}

	ctx, err := h.requireContext(l)
	if err != nil {
		return pushError(l, err)
	}
	if !security.IsAllowed(ctx, "hub.dependents.get", moduleKey, nil) {
		return pushError(l, permissionDenied(l, "hub.dependents.get", moduleKey))
	}

	base, opts, err := parseDependentsOptions(l, 2)
	if err != nil {
		return pushError(l, err)
	}

	client, err := h.moduleClient(l, base)
	if err != nil {
		return pushError(l, err)
	}

	ctx, cancel := withTimeout(ctx, base.timeout)
	defer cancel()

	req := &modulev1.GetDependentsRequest{
		Module:   moduleRef,
		Page:     opts.page,
		PageSize: opts.pageSize,
	}
	resp, callErr := client.GetDependents(ctx, connect.NewRequest(req))
	if callErr != nil {
		return pushError(l, hubCallError(l, callErr))
	}

	items := lua.CreateTable(len(resp.Msg.Dependents), 0)
	for i, dep := range resp.Msg.Dependents {
		items.RawSetInt(i+1, dependentToTable(l, dep))
	}
	result := lua.CreateTable(0, 4)
	result.RawSetString("items", items)
	result.RawSetString("total", lua.LNumber(resp.Msg.GetTotal()))
	result.RawSetString("page", lua.LNumber(resp.Msg.GetPage()))
	result.RawSetString("page_size", lua.LNumber(resp.Msg.GetPageSize()))
	l.Push(result)
	return 1
}

func (h *hubModule) filesList(l *lua.LState) int {
	moduleRef, moduleKey, err := parseModuleRef(l, 1)
	if err != nil {
		return pushError(l, err)
	}
	versionRef, err := parseVersionRef(l, 2)
	if err != nil {
		return pushError(l, err)
	}

	ctx, err := h.requireContext(l)
	if err != nil {
		return pushError(l, err)
	}
	if !security.IsAllowed(ctx, "hub.files.list", moduleKey, nil) {
		return pushError(l, permissionDenied(l, "hub.files.list", moduleKey))
	}

	base, opts, err := parseFilesOptions(l, 3)
	if err != nil {
		return pushError(l, err)
	}

	client, err := h.moduleClient(l, base)
	if err != nil {
		return pushError(l, err)
	}

	ctx, cancel := withTimeout(ctx, base.timeout)
	defer cancel()

	req := &modulev1.ListVersionFilesRequest{
		Module:   moduleRef,
		Version:  versionRef,
		Page:     opts.page,
		PageSize: opts.pageSize,
	}
	resp, callErr := client.ListVersionFiles(ctx, connect.NewRequest(req))
	if callErr != nil {
		return pushError(l, hubCallError(l, callErr))
	}

	items := lua.CreateTable(len(resp.Msg.Files), 0)
	for i, file := range resp.Msg.Files {
		items.RawSetInt(i+1, versionFileToTable(l, file))
	}
	result := lua.CreateTable(0, 4)
	result.RawSetString("items", items)
	result.RawSetString("total", lua.LNumber(resp.Msg.GetTotal()))
	result.RawSetString("page", lua.LNumber(resp.Msg.GetPage()))
	result.RawSetString("page_size", lua.LNumber(resp.Msg.GetPageSize()))
	l.Push(result)
	return 1
}

// Helpers

type baseOptions struct {
	registry string
	token    string
	timeout  time.Duration
}

type listModulesOptions struct {
	organizationID string
	page           int32
	pageSize       int32
	visibility     modulev1.Visibility
	moduleType     modulev1.ModuleType
	sortOrder      modulev1.ModuleSortOrder
}

type searchModulesOptions struct {
	license           string
	keywords          []string
	page              int32
	pageSize          int32
	includeDeprecated bool
}

type listVersionsOptions struct {
	page          int32
	pageSize      int32
	includeYanked bool
}

type dependentsOptions struct {
	page     int32
	pageSize int32
}

type filesOptions struct {
	page     int32
	pageSize int32
}

type readmeOptions struct {
	version *versionv1.VersionRef
}

func (h *hubModule) requireContext(l *lua.LState) (context.Context, *lua.Error) {
	ctx := l.Context()
	if ctx == nil {
		return nil, lua.NewLuaError(l, "no context").WithKind(lua.Internal).WithRetryable(false)
	}
	return ctx, nil
}

func (h *hubModule) moduleClient(l *lua.LState, base baseOptions) (modulev1connect.ModuleServiceClient, *lua.Error) {
	if h.opts.ModuleClient != nil {
		return h.opts.ModuleClient, nil
	}

	client, err := h.newHubClient(l, base)
	if err != nil {
		return nil, err
	}
	return client.Module, nil
}

func (h *hubModule) artifactClient(l *lua.LState, base baseOptions) (ArtifactClient, *lua.Error) {
	if h.opts.ArtifactClient != nil {
		return h.opts.ArtifactClient, nil
	}

	return h.newHubClient(l, base)
}

func (h *hubModule) newHubClient(l *lua.LState, base baseOptions) (*boothub.Client, *lua.Error) {
	var store *bootauth.Store
	registry := firstNonEmpty(base.registry, h.opts.BaseURL)
	if registry == "" {
		store = h.authStore()
		if store != nil {
			registry = store.DefaultRegistry()
		}
	}

	if registry == "" {
		return nil, lua.NewLuaError(l, "registry URL required").WithKind(lua.Invalid).WithRetryable(false)
	}

	token := firstNonEmpty(base.token, h.opts.Token)
	if token == "" {
		if store == nil {
			store = h.authStore()
		}
		if store != nil {
			cred, _ := store.Get(registry)
			if cred != nil {
				token = cred.Token
			}
		}
	}

	timeout := base.timeout
	if timeout == 0 {
		timeout = h.opts.Timeout
	}

	client, err := boothub.NewClient(boothub.Options{
		BaseURL: registry,
		Token:   token,
		Timeout: timeout,
	})
	if err != nil {
		return nil, lua.WrapErrorWithLua(l, err, "hub client init").WithKind(lua.Invalid).WithRetryable(false)
	}

	return client, nil
}

func withTimeout(ctx context.Context, timeout time.Duration) (context.Context, func()) {
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

func parseListModulesOptions(l *lua.LState, idx int) (baseOptions, listModulesOptions, *lua.Error) {
	base, tbl, err := parseOptionsTable(l, idx)
	if err != nil {
		return baseOptions{}, listModulesOptions{}, err
	}
	opts := listModulesOptions{}
	if tbl == nil {
		return base, opts, nil
	}

	if org, ok, err := tableString(l, tbl, "organization_id"); err != nil {
		return base, opts, err
	} else if ok {
		opts.organizationID = org
	} else if org, ok, err := tableString(l, tbl, "org"); err != nil {
		return base, opts, err
	} else if ok {
		opts.organizationID = org
	}

	if page, ok, err := tableInt32(l, tbl, "page"); err != nil {
		return base, opts, err
	} else if ok {
		opts.page = page
	}

	if pageSize, ok, err := tableInt32(l, tbl, "page_size"); err != nil {
		return base, opts, err
	} else if ok {
		opts.pageSize = pageSize
	}

	if visibility, ok, err := tableEnumVisibility(l, tbl, "visibility"); err != nil {
		return base, opts, err
	} else if ok {
		opts.visibility = visibility
	}

	if moduleType, ok, err := tableEnumModuleType(l, tbl, "type"); err != nil {
		return base, opts, err
	} else if ok {
		opts.moduleType = moduleType
	}

	if sortOrder, ok, err := tableEnumSortOrder(l, tbl, "sort_order"); err != nil {
		return base, opts, err
	} else if ok {
		opts.sortOrder = sortOrder
	} else if sortOrder, ok, err := tableEnumSortOrder(l, tbl, "sort"); err != nil {
		return base, opts, err
	} else if ok {
		opts.sortOrder = sortOrder
	}

	return base, opts, nil
}

func parseSearchModulesOptions(l *lua.LState, idx int) (baseOptions, searchModulesOptions, *lua.Error) {
	base, tbl, err := parseOptionsTable(l, idx)
	if err != nil {
		return baseOptions{}, searchModulesOptions{}, err
	}
	opts := searchModulesOptions{}
	if tbl == nil {
		return base, opts, nil
	}

	if page, ok, err := tableInt32(l, tbl, "page"); err != nil {
		return base, opts, err
	} else if ok {
		opts.page = page
	}

	if pageSize, ok, err := tableInt32(l, tbl, "page_size"); err != nil {
		return base, opts, err
	} else if ok {
		opts.pageSize = pageSize
	}

	if keywords, ok, err := tableStringSlice(l, tbl, "keywords"); err != nil {
		return base, opts, err
	} else if ok {
		opts.keywords = keywords
	}

	if license, ok, err := tableString(l, tbl, "license"); err != nil {
		return base, opts, err
	} else if ok {
		opts.license = license
	}

	if includeDeprecated, ok, err := tableBool(l, tbl, "include_deprecated"); err != nil {
		return base, opts, err
	} else if ok {
		opts.includeDeprecated = includeDeprecated
	}

	return base, opts, nil
}

func parseListVersionsOptions(l *lua.LState, idx int) (baseOptions, listVersionsOptions, *lua.Error) {
	base, tbl, err := parseOptionsTable(l, idx)
	if err != nil {
		return baseOptions{}, listVersionsOptions{}, err
	}
	opts := listVersionsOptions{}
	if tbl == nil {
		return base, opts, nil
	}

	if page, ok, err := tableInt32(l, tbl, "page"); err != nil {
		return base, opts, err
	} else if ok {
		opts.page = page
	}

	if pageSize, ok, err := tableInt32(l, tbl, "page_size"); err != nil {
		return base, opts, err
	} else if ok {
		opts.pageSize = pageSize
	}

	if includeYanked, ok, err := tableBool(l, tbl, "include_yanked"); err != nil {
		return base, opts, err
	} else if ok {
		opts.includeYanked = includeYanked
	}

	return base, opts, nil
}

func parseDependentsOptions(l *lua.LState, idx int) (baseOptions, dependentsOptions, *lua.Error) {
	base, tbl, err := parseOptionsTable(l, idx)
	if err != nil {
		return baseOptions{}, dependentsOptions{}, err
	}
	opts := dependentsOptions{}
	if tbl == nil {
		return base, opts, nil
	}

	if page, ok, err := tableInt32(l, tbl, "page"); err != nil {
		return base, opts, err
	} else if ok {
		opts.page = page
	}

	if pageSize, ok, err := tableInt32(l, tbl, "page_size"); err != nil {
		return base, opts, err
	} else if ok {
		opts.pageSize = pageSize
	}

	return base, opts, nil
}

func parseFilesOptions(l *lua.LState, idx int) (baseOptions, filesOptions, *lua.Error) {
	base, tbl, err := parseOptionsTable(l, idx)
	if err != nil {
		return baseOptions{}, filesOptions{}, err
	}
	opts := filesOptions{}
	if tbl == nil {
		return base, opts, nil
	}

	if page, ok, err := tableInt32(l, tbl, "page"); err != nil {
		return base, opts, err
	} else if ok {
		opts.page = page
	}

	if pageSize, ok, err := tableInt32(l, tbl, "page_size"); err != nil {
		return base, opts, err
	} else if ok {
		opts.pageSize = pageSize
	}

	return base, opts, nil
}

func parseReadmeOptions(l *lua.LState, idx int) (baseOptions, readmeOptions, *lua.Error) {
	base, tbl, err := parseOptionsTable(l, idx)
	if err != nil {
		return baseOptions{}, readmeOptions{}, err
	}
	opts := readmeOptions{}
	if tbl == nil {
		return base, opts, nil
	}

	if versionVal := tbl.RawGetString("version"); versionVal != lua.LNil {
		versionRef, err := parseVersionRefFromValue(l, versionVal, "version")
		if err != nil {
			return base, opts, err
		}
		opts.version = versionRef
	}

	return base, opts, nil
}

func parseBaseOptions(l *lua.LState, idx int) (baseOptions, *lua.Error) {
	base, _, err := parseOptionsTable(l, idx)
	if err != nil {
		return baseOptions{}, err
	}
	return base, nil
}

func parseOptionsTable(l *lua.LState, idx int) (baseOptions, *lua.LTable, *lua.Error) {
	base := baseOptions{}
	if l.GetTop() < idx {
		return base, nil, nil
	}

	val := l.Get(idx)
	if val == lua.LNil {
		return base, nil, nil
	}

	tbl, ok := val.(*lua.LTable)
	if !ok {
		return base, nil, invalidOptionError(l, "options", "table", val)
	}

	if registry, ok, err := tableString(l, tbl, "registry"); err != nil {
		return base, nil, err
	} else if ok {
		base.registry = registry
	}

	if token, ok, err := tableString(l, tbl, "token"); err != nil {
		return base, nil, err
	} else if ok {
		base.token = token
	}

	if timeout, ok, err := tableDuration(l, tbl, "timeout"); err != nil {
		return base, nil, err
	} else if ok {
		base.timeout = timeout
	}

	return base, tbl, nil
}

func parseModuleRef(l *lua.LState, idx int) (*modulev1.ModuleRef, string, *lua.Error) {
	val := l.Get(idx)
	if val == lua.LNil {
		return nil, "", invalidArgument(l, "module reference required")
	}

	switch v := val.(type) {
	case lua.LString:
		ref, key, err := moduleRefFromString(v.String())
		if err != nil {
			return nil, "", invalidArgument(l, err.Error())
		}
		return ref, key, nil
	case *lua.LTable:
		return moduleRefFromTable(l, v)
	default:
		return nil, "", invalidArgument(l, "module reference must be string or table")
	}
}

func moduleRefFromString(raw string) (*modulev1.ModuleRef, string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return nil, "", fmt.Errorf("module reference required")
	}

	if strings.Contains(name, "/") {
		parts := strings.SplitN(name, "/", 2)
		if parts[0] == "" || parts[1] == "" {
			return nil, "", fmt.Errorf("module name must be org/name")
		}
		return &modulev1.ModuleRef{Value: &modulev1.ModuleRef_Name{Name: &modulev1.ModuleName{Org: parts[0], Name: parts[1]}}}, name, nil
	}

	return &modulev1.ModuleRef{Value: &modulev1.ModuleRef_Id{Id: name}}, name, nil
}

func moduleRefFromTable(l *lua.LState, tbl *lua.LTable) (*modulev1.ModuleRef, string, *lua.Error) {
	if id, ok, err := tableString(l, tbl, "id"); err != nil {
		return nil, "", err
	} else if ok {
		return &modulev1.ModuleRef{Value: &modulev1.ModuleRef_Id{Id: id}}, id, nil
	}

	org, okOrg, err := tableString(l, tbl, "org")
	if err != nil {
		return nil, "", err
	}
	name, okName, err := tableString(l, tbl, "name")
	if err != nil {
		return nil, "", err
	}
	if okOrg && okName {
		key := fmt.Sprintf("%s/%s", org, name)
		return &modulev1.ModuleRef{Value: &modulev1.ModuleRef_Name{Name: &modulev1.ModuleName{Org: org, Name: name}}}, key, nil
	}

	if full, ok, err := tableString(l, tbl, "full_name"); err != nil {
		return nil, "", err
	} else if ok {
		ref, key, err := moduleRefFromString(full)
		if err != nil {
			return nil, "", invalidArgument(l, err.Error())
		}
		return ref, key, nil
	}

	return nil, "", invalidArgument(l, "module reference table must include id or org/name")
}

func parseVersionRef(l *lua.LState, idx int) (*versionv1.VersionRef, *lua.Error) {
	val := l.Get(idx)
	if val == lua.LNil {
		return nil, invalidArgument(l, "version reference required")
	}
	return parseVersionRefFromValue(l, val, "version")
}

func parseOptionalVersionRef(l *lua.LState, idx int) (*versionv1.VersionRef, *lua.Error) {
	if l.GetTop() < idx {
		return nil, nil
	}
	val := l.Get(idx)
	if val == lua.LNil {
		return nil, nil
	}
	return parseVersionRefFromValue(l, val, "version")
}

func parseVersionRefFromValue(l *lua.LState, val lua.LValue, field string) (*versionv1.VersionRef, *lua.Error) {
	switch v := val.(type) {
	case lua.LString:
		ver := strings.TrimSpace(v.String())
		if ver == "" {
			return nil, invalidArgument(l, "version reference required")
		}
		return &versionv1.VersionRef{Value: &versionv1.VersionRef_Version{Version: ver}}, nil
	case *lua.LTable:
		if id, ok, err := tableString(l, v, "id"); err != nil {
			return nil, err
		} else if ok {
			return &versionv1.VersionRef{Value: &versionv1.VersionRef_Id{Id: id}}, nil
		}
		if version, ok, err := tableString(l, v, "version"); err != nil {
			return nil, err
		} else if ok {
			return &versionv1.VersionRef{Value: &versionv1.VersionRef_Version{Version: version}}, nil
		}
		if label, ok, err := tableString(l, v, "label"); err != nil {
			return nil, err
		} else if ok {
			return &versionv1.VersionRef{Value: &versionv1.VersionRef_Label{Label: label}}, nil
		}
		return nil, invalidArgument(l, fmt.Sprintf("%s table must include id, version, or label", field))
	default:
		return nil, invalidArgument(l, fmt.Sprintf("%s reference must be string or table", field))
	}
}

func tableString(l *lua.LState, tbl *lua.LTable, key string) (string, bool, *lua.Error) {
	val := tbl.RawGetString(key)
	if val == lua.LNil {
		return "", false, nil
	}
	str, ok := val.(lua.LString)
	if !ok {
		return "", false, invalidOptionError(l, key, "string", val)
	}
	return string(str), true, nil
}

func tableBool(l *lua.LState, tbl *lua.LTable, key string) (bool, bool, *lua.Error) {
	val := tbl.RawGetString(key)
	if val == lua.LNil {
		return false, false, nil
	}
	b, ok := val.(lua.LBool)
	if !ok {
		return false, false, invalidOptionError(l, key, "boolean", val)
	}
	return bool(b), true, nil
}

func tableInt32(l *lua.LState, tbl *lua.LTable, key string) (int32, bool, *lua.Error) {
	val := tbl.RawGetString(key)
	if val == lua.LNil {
		return 0, false, nil
	}
	if num, ok := val.(lua.LNumber); ok {
		return int32(num), true, nil
	}
	if num, ok := val.(lua.LInteger); ok {
		return int32(num), true, nil
	}
	return 0, false, invalidOptionError(l, key, "number", val)
}

func tableDuration(l *lua.LState, tbl *lua.LTable, key string) (time.Duration, bool, *lua.Error) {
	val := tbl.RawGetString(key)
	if val == lua.LNil {
		return 0, false, nil
	}
	dur, ok := parseDuration(val)
	if !ok {
		return 0, false, invalidOptionError(l, key, "duration", val)
	}
	return dur, true, nil
}

func tableStringSlice(l *lua.LState, tbl *lua.LTable, key string) ([]string, bool, *lua.Error) {
	val := tbl.RawGetString(key)
	if val == lua.LNil {
		return nil, false, nil
	}
	table, ok := val.(*lua.LTable)
	if !ok {
		return nil, false, invalidOptionError(l, key, "array", val)
	}
	items := make([]string, 0, table.Len())
	var typeErr *lua.Error
	table.ForEach(func(_, v lua.LValue) {
		if typeErr != nil {
			return
		}
		str, ok := v.(lua.LString)
		if !ok {
			typeErr = invalidOptionError(l, key, "array of strings", v)
			return
		}
		items = append(items, string(str))
	})
	if typeErr != nil {
		return nil, false, typeErr
	}
	return items, true, nil
}

func tableEnumVisibility(l *lua.LState, tbl *lua.LTable, key string) (modulev1.Visibility, bool, *lua.Error) {
	val := tbl.RawGetString(key)
	if val == lua.LNil {
		return modulev1.Visibility_VISIBILITY_UNSPECIFIED, false, nil
	}

	switch v := val.(type) {
	case lua.LNumber:
		return modulev1.Visibility(v), true, nil
	case lua.LInteger:
		return modulev1.Visibility(v), true, nil
	case lua.LString:
		visibility, ok := parseVisibility(string(v))
		if !ok {
			return modulev1.Visibility_VISIBILITY_UNSPECIFIED, false, invalidOptionError(l, key, "visibility", val)
		}
		return visibility, true, nil
	default:
		return modulev1.Visibility_VISIBILITY_UNSPECIFIED, false, invalidOptionError(l, key, "visibility", val)
	}
}

func tableEnumModuleType(l *lua.LState, tbl *lua.LTable, key string) (modulev1.ModuleType, bool, *lua.Error) {
	val := tbl.RawGetString(key)
	if val == lua.LNil {
		return modulev1.ModuleType_MODULE_TYPE_UNSPECIFIED, false, nil
	}

	switch v := val.(type) {
	case lua.LNumber:
		return modulev1.ModuleType(v), true, nil
	case lua.LInteger:
		return modulev1.ModuleType(v), true, nil
	case lua.LString:
		moduleType, ok := parseModuleType(string(v))
		if !ok {
			return modulev1.ModuleType_MODULE_TYPE_UNSPECIFIED, false, invalidOptionError(l, key, "module type", val)
		}
		return moduleType, true, nil
	default:
		return modulev1.ModuleType_MODULE_TYPE_UNSPECIFIED, false, invalidOptionError(l, key, "module type", val)
	}
}

func tableEnumSortOrder(l *lua.LState, tbl *lua.LTable, key string) (modulev1.ModuleSortOrder, bool, *lua.Error) {
	val := tbl.RawGetString(key)
	if val == lua.LNil {
		return modulev1.ModuleSortOrder_MODULE_SORT_ORDER_UNSPECIFIED, false, nil
	}

	switch v := val.(type) {
	case lua.LNumber:
		return modulev1.ModuleSortOrder(v), true, nil
	case lua.LInteger:
		return modulev1.ModuleSortOrder(v), true, nil
	case lua.LString:
		sortOrder, ok := parseSortOrder(string(v))
		if !ok {
			return modulev1.ModuleSortOrder_MODULE_SORT_ORDER_UNSPECIFIED, false, invalidOptionError(l, key, "sort order", val)
		}
		return sortOrder, true, nil
	default:
		return modulev1.ModuleSortOrder_MODULE_SORT_ORDER_UNSPECIFIED, false, invalidOptionError(l, key, "sort order", val)
	}
}

func parseVisibility(value string) (modulev1.Visibility, bool) {
	v := normalizeEnum(value)
	switch v {
	case "public":
		return modulev1.Visibility_VISIBILITY_PUBLIC, true
	case "private":
		return modulev1.Visibility_VISIBILITY_PRIVATE, true
	case "internal":
		return modulev1.Visibility_VISIBILITY_INTERNAL, true
	case "", "unspecified", "any", "all":
		return modulev1.Visibility_VISIBILITY_UNSPECIFIED, true
	default:
		return modulev1.Visibility_VISIBILITY_UNSPECIFIED, false
	}
}

func parseModuleType(value string) (modulev1.ModuleType, bool) {
	v := normalizeEnum(value)
	switch v {
	case "library", "lib":
		return modulev1.ModuleType_MODULE_TYPE_LIBRARY, true
	case "application", "app":
		return modulev1.ModuleType_MODULE_TYPE_APPLICATION, true
	case "agent":
		return modulev1.ModuleType_MODULE_TYPE_AGENT, true
	case "plugin":
		return modulev1.ModuleType_MODULE_TYPE_PLUGIN, true
	case "", "unspecified", "any", "all":
		return modulev1.ModuleType_MODULE_TYPE_UNSPECIFIED, true
	default:
		return modulev1.ModuleType_MODULE_TYPE_UNSPECIFIED, false
	}
}

func parseSortOrder(value string) (modulev1.ModuleSortOrder, bool) {
	v := normalizeEnum(value)
	switch v {
	case "name_asc", "name-asc":
		return modulev1.ModuleSortOrder_MODULE_SORT_ORDER_NAME_ASC, true
	case "name_desc", "name-desc":
		return modulev1.ModuleSortOrder_MODULE_SORT_ORDER_NAME_DESC, true
	case "created_desc", "created-desc":
		return modulev1.ModuleSortOrder_MODULE_SORT_ORDER_CREATED_DESC, true
	case "updated_desc", "updated-desc":
		return modulev1.ModuleSortOrder_MODULE_SORT_ORDER_UPDATED_DESC, true
	case "downloads_desc", "downloads-desc", "downloads":
		return modulev1.ModuleSortOrder_MODULE_SORT_ORDER_DOWNLOADS_DESC, true
	case "", "unspecified", "any", "all":
		return modulev1.ModuleSortOrder_MODULE_SORT_ORDER_UNSPECIFIED, true
	default:
		return modulev1.ModuleSortOrder_MODULE_SORT_ORDER_UNSPECIFIED, false
	}
}

func normalizeEnum(value string) string {
	v := strings.TrimSpace(strings.ToLower(value))
	v = strings.ReplaceAll(v, " ", "_")
	return v
}

func parseDuration(val lua.LValue) (time.Duration, bool) {
	switch v := val.(type) {
	case lua.LString:
		d, err := time.ParseDuration(string(v))
		if err != nil {
			return 0, false
		}
		return d, true
	case lua.LNumber:
		return time.Duration(v) * time.Second, true
	case lua.LInteger:
		return time.Duration(v) * time.Second, true
	default:
		return 0, false
	}
}

func pushListResponse(l *lua.LState, modules []*modulev1.Module, total, page, pageSize int32) int {
	items := lua.CreateTable(len(modules), 0)
	for i, m := range modules {
		items.RawSetInt(i+1, moduleToTable(l, m))
	}

	result := lua.CreateTable(0, 4)
	result.RawSetString("items", items)
	result.RawSetString("total", lua.LNumber(total))
	result.RawSetString("page", lua.LNumber(page))
	result.RawSetString("page_size", lua.LNumber(pageSize))
	l.Push(result)
	return 1
}

func pushVersionListResponse(l *lua.LState, versions []*versionv1.Version, total, page, pageSize int32) int {
	items := lua.CreateTable(len(versions), 0)
	for i, v := range versions {
		items.RawSetInt(i+1, versionToTable(l, v))
	}

	result := lua.CreateTable(0, 4)
	result.RawSetString("items", items)
	result.RawSetString("total", lua.LNumber(total))
	result.RawSetString("page", lua.LNumber(page))
	result.RawSetString("page_size", lua.LNumber(pageSize))
	l.Push(result)
	return 1
}

func moduleToTable(l *lua.LState, m *modulev1.Module) *lua.LTable {
	if m == nil {
		return lua.CreateTable(0, 0)
	}

	result := lua.CreateTable(0, 22)
	result.RawSetString("id", lua.LString(m.GetId()))
	result.RawSetString("name", lua.LString(m.GetName()))
	result.RawSetString("org", lua.LString(m.GetOrganizationName()))
	result.RawSetString("org_id", lua.LString(m.GetOrganizationId()))
	if m.GetOrganizationName() != "" && m.GetName() != "" {
		result.RawSetString("full_name", lua.LString(m.GetOrganizationName()+"/"+m.GetName()))
	}
	result.RawSetString("display_name", lua.LString(m.GetDisplayName()))
	result.RawSetString("description", lua.LString(m.GetDescription()))
	result.RawSetString("latest_version", lua.LString(m.GetLatestVersion()))
	result.RawSetString("total_downloads", lua.LNumber(m.GetTotalDownloads()))
	result.RawSetString("favorites_count", lua.LNumber(m.GetFavoritesCount()))
	result.RawSetString("create_time", lua.LString(formatTime(m.GetCreateTime())))
	result.RawSetString("update_time", lua.LString(formatTime(m.GetUpdateTime())))
	result.RawSetString("visibility", lua.LString(visibilityString(m.GetVisibility())))
	result.RawSetString("deprecated", lua.LBool(m.GetDeprecated()))
	result.RawSetString("deprecation_message", lua.LString(m.GetDeprecationMessage()))
	result.RawSetString("type", lua.LString(moduleTypeString(m.GetType())))
	result.RawSetString("keywords", stringSliceToTable(l, m.GetKeywords()))
	result.RawSetString("license", lua.LString(m.GetLicense()))
	result.RawSetString("repository", lua.LString(m.GetRepository()))
	result.RawSetString("homepage", lua.LString(m.GetHomepage()))
	result.RawSetString("protected", lua.LBool(m.GetProtected()))
	result.RawSetString("contracts", contractsToTable(l, m.GetContracts()))
	result.RawSetString("download_stats", downloadStatsToTable(l, m.GetDownloadStats()))
	return result
}

func versionToTable(l *lua.LState, v *versionv1.Version) *lua.LTable {
	if v == nil {
		return lua.CreateTable(0, 0)
	}

	result := lua.CreateTable(0, 24)
	result.RawSetString("id", lua.LString(v.GetId()))
	result.RawSetString("module_id", lua.LString(v.GetModuleId()))
	result.RawSetString("version", lua.LString(v.GetVersion()))
	result.RawSetString("digest", lua.LString(v.GetDigest()))
	result.RawSetString("size_bytes", lua.LNumber(v.GetSizeBytes()))
	result.RawSetString("yanked", lua.LBool(v.GetYanked()))
	result.RawSetString("published_by", lua.LString(v.GetPublishedBy()))
	result.RawSetString("create_time", lua.LString(formatTime(v.GetCreateTime())))
	result.RawSetString("download_count", lua.LNumber(v.GetDownloadCount()))
	result.RawSetString("protected", lua.LBool(v.GetProtected()))
	result.RawSetString("protection_type", lua.LString(v.GetProtectionType().String()))
	result.RawSetString("lua_modules", stringSliceToTable(l, v.GetLuaModules()))
	result.RawSetString("entry_kinds", stringSliceToTable(l, v.GetEntryKinds()))
	result.RawSetString("entry_count", lua.LNumber(v.GetEntryCount()))
	result.RawSetString("dependencies", dependenciesToTable(l, v.GetDependencies()))
	result.RawSetString("requirements", requirementsToTable(l, v.GetRequirements()))
	result.RawSetString("files", versionFilesToTable(l, v.GetFiles()))
	result.RawSetString("readme", lua.LString(v.GetReadme()))
	result.RawSetString("release_notes", lua.LString(v.GetReleaseNotes()))
	result.RawSetString("source", lua.LString(v.GetSource()))
	result.RawSetString("source_label", lua.LString(v.GetSourceLabel()))
	result.RawSetString("metadata", versionMetadataToTable(l, v.GetMetadata()))
	return result
}

func artifactInspectionToTable(l *lua.LState, inspection *artifactInspection) *lua.LTable {
	result := lua.CreateTable(0, 7)
	if inspection == nil {
		return result
	}
	result.RawSetString("version", lua.LString(inspection.Version))
	result.RawSetString("digest", lua.LString(inspection.Digest))
	result.RawSetString("size_bytes", lua.LNumber(inspection.SizeBytes))
	result.RawSetString("protected", lua.LBool(inspection.Protected))
	result.RawSetString("entry_count", lua.LNumber(inspection.EntryCount))
	result.RawSetString("entry_kinds", stringSliceToTable(l, inspection.EntryKinds))
	result.RawSetString("requirements", requirementsToTable(l, inspection.Requirements))
	result.RawSetString("cache_path", lua.LString(inspection.Path))
	return result
}

func dependencyToTable(_ *lua.LState, dep *versionv1.Dependency) *lua.LTable {
	result := lua.CreateTable(0, 3)
	if dep == nil {
		return result
	}
	result.RawSetString("org", lua.LString(dep.GetOrg()))
	result.RawSetString("name", lua.LString(dep.GetName()))
	result.RawSetString("version_constraint", lua.LString(dep.GetVersionConstraint()))
	return result
}

func dependentToTable(_ *lua.LState, dep *modulev1.DependentModule) *lua.LTable {
	result := lua.CreateTable(0, 4)
	if dep == nil {
		return result
	}
	result.RawSetString("org", lua.LString(dep.GetOrg()))
	result.RawSetString("name", lua.LString(dep.GetName()))
	result.RawSetString("version", lua.LString(dep.GetVersion()))
	result.RawSetString("constraint", lua.LString(dep.GetConstraint()))
	return result
}

func versionFileToTable(_ *lua.LState, file *versionv1.VersionFile) *lua.LTable {
	result := lua.CreateTable(0, 2)
	if file == nil {
		return result
	}
	result.RawSetString("path", lua.LString(file.GetPath()))
	result.RawSetString("size_bytes", lua.LNumber(file.GetSizeBytes()))
	return result
}

func contractsToTable(l *lua.LState, contracts []*modulev1.Contract) *lua.LTable {
	items := lua.CreateTable(len(contracts), 0)
	for i, contract := range contracts {
		items.RawSetInt(i+1, contractToTable(l, contract))
	}
	return items
}

func contractToTable(l *lua.LState, contract *modulev1.Contract) *lua.LTable {
	result := lua.CreateTable(0, 4)
	if contract == nil {
		return result
	}
	result.RawSetString("id", lua.LString(contract.GetId()))
	result.RawSetString("name", lua.LString(contract.GetName()))
	result.RawSetString("description", lua.LString(contract.GetDescription()))
	methods := lua.CreateTable(len(contract.GetMethods()), 0)
	for i, method := range contract.GetMethods() {
		methods.RawSetInt(i+1, contractMethodToTable(l, method))
	}
	result.RawSetString("methods", methods)
	return result
}

func contractMethodToTable(_ *lua.LState, method *modulev1.ContractMethod) *lua.LTable {
	result := lua.CreateTable(0, 2)
	if method == nil {
		return result
	}
	result.RawSetString("name", lua.LString(method.GetName()))
	result.RawSetString("description", lua.LString(method.GetDescription()))
	return result
}

func downloadStatsToTable(_ *lua.LState, stats []*modulev1.DownloadStat) *lua.LTable {
	items := lua.CreateTable(len(stats), 0)
	for i, stat := range stats {
		result := lua.CreateTable(0, 2)
		if stat != nil {
			result.RawSetString("date", lua.LString(stat.GetDate()))
			result.RawSetString("count", lua.LNumber(stat.GetCount()))
		}
		items.RawSetInt(i+1, result)
	}
	return items
}

func dependenciesToTable(l *lua.LState, deps []*versionv1.Dependency) *lua.LTable {
	items := lua.CreateTable(len(deps), 0)
	for i, dep := range deps {
		items.RawSetInt(i+1, dependencyToTable(l, dep))
	}
	return items
}

func requirementsToTable(l *lua.LState, reqs []*versionv1.Requirement) *lua.LTable {
	items := lua.CreateTable(len(reqs), 0)
	for i, req := range reqs {
		result := lua.CreateTable(0, 4)
		if req != nil {
			result.RawSetString("name", lua.LString(req.GetName()))
			result.RawSetString("description", lua.LString(req.GetDescription()))
			result.RawSetString("default", lua.LString(req.GetDefault()))
			result.RawSetString("targets", requirementTargetsToTable(l, req.GetTargets()))
		}
		items.RawSetInt(i+1, result)
	}
	return items
}

func requirementTargetsToTable(_ *lua.LState, targets []*versionv1.RequirementTarget) *lua.LTable {
	items := lua.CreateTable(len(targets), 0)
	for i, target := range targets {
		result := lua.CreateTable(0, 2)
		if target != nil {
			result.RawSetString("entry", lua.LString(target.GetEntry()))
			result.RawSetString("path", lua.LString(target.GetPath()))
		}
		items.RawSetInt(i+1, result)
	}
	return items
}

func versionFilesToTable(l *lua.LState, files []*versionv1.VersionFile) *lua.LTable {
	items := lua.CreateTable(len(files), 0)
	for i, file := range files {
		items.RawSetInt(i+1, versionFileToTable(l, file))
	}
	return items
}

func versionMetadataToTable(l *lua.LState, meta *versionv1.VersionMetadata) *lua.LTable {
	result := lua.CreateTable(0, 8)
	if meta == nil {
		return result
	}
	result.RawSetString("description", lua.LString(meta.GetDescription()))
	result.RawSetString("repository", lua.LString(meta.GetRepository()))
	result.RawSetString("license", lua.LString(meta.GetLicense()))
	result.RawSetString("keywords", stringSliceToTable(l, meta.GetKeywords()))
	result.RawSetString("authors", stringSliceToTable(l, meta.GetAuthors()))
	result.RawSetString("documentation", lua.LString(meta.GetDocumentation()))
	result.RawSetString("homepage", lua.LString(meta.GetHomepage()))
	result.RawSetString("min_wippy_version", lua.LString(meta.GetMinWippyVersion()))
	return result
}

func stringSliceToTable(_ *lua.LState, values []string) *lua.LTable {
	items := lua.CreateTable(len(values), 0)
	for i, value := range values {
		items.RawSetInt(i+1, lua.LString(value))
	}
	return items
}

func formatTime(ts interface{ AsTime() time.Time }) string {
	if ts == nil {
		return ""
	}
	return ts.AsTime().UTC().Format(time.RFC3339)
}

func visibilityString(v modulev1.Visibility) string {
	switch v {
	case modulev1.Visibility_VISIBILITY_PUBLIC:
		return "public"
	case modulev1.Visibility_VISIBILITY_PRIVATE:
		return "private"
	case modulev1.Visibility_VISIBILITY_INTERNAL:
		return "internal"
	default:
		return "unspecified"
	}
}

func moduleTypeString(t modulev1.ModuleType) string {
	switch t {
	case modulev1.ModuleType_MODULE_TYPE_LIBRARY:
		return "library"
	case modulev1.ModuleType_MODULE_TYPE_APPLICATION:
		return "application"
	case modulev1.ModuleType_MODULE_TYPE_AGENT:
		return "agent"
	case modulev1.ModuleType_MODULE_TYPE_PLUGIN:
		return "plugin"
	default:
		return "unspecified"
	}
}

func pushError(l *lua.LState, err *lua.Error) int {
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func invalidArgument(l *lua.LState, msg string) *lua.Error {
	return lua.NewLuaError(l, msg).WithKind(lua.Invalid).WithRetryable(false)
}

func permissionDenied(l *lua.LState, action, resource string) *lua.Error {
	return lua.NewLuaError(l, "not allowed: "+action).WithKind(lua.PermissionDenied).WithRetryable(false).WithDetails(map[string]any{
		"action":   action,
		"resource": resource,
	})
}

func invalidOptionError(l *lua.LState, key, expected string, got lua.LValue) *lua.Error {
	return lua.NewLuaError(l, fmt.Sprintf("invalid option %q", key)).WithKind(lua.Invalid).WithRetryable(false).WithDetails(map[string]any{
		"option":   key,
		"expected": expected,
		"actual":   got.Type().String(),
	})
}

func hubCallError(l *lua.LState, err error) *lua.Error {
	if err == nil {
		return nil
	}

	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		luaErr := lua.WrapErrorWithLua(l, connectErr, "hub request failed")
		kind := mapConnectError(connectErr.Code())
		return luaErr.WithKind(kind).WithRetryable(isRetryableConnect(connectErr.Code())).WithDetails(map[string]any{
			"code": connectErr.Code().String(),
		})
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return lua.WrapErrorWithLua(l, err, "hub request timed out").WithKind(lua.Timeout).WithRetryable(true)
	}
	if errors.Is(err, context.Canceled) {
		return lua.WrapErrorWithLua(l, err, "hub request canceled").WithKind(lua.Canceled).WithRetryable(false)
	}

	return lua.WrapErrorWithLua(l, err, "hub request failed").WithKind(lua.Internal).WithRetryable(false)
}

func mapConnectError(code connect.Code) lua.Kind {
	switch code {
	case connect.CodeInvalidArgument:
		return lua.Invalid
	case connect.CodeNotFound:
		return lua.NotFound
	case connect.CodeAlreadyExists:
		return lua.AlreadyExists
	case connect.CodePermissionDenied, connect.CodeUnauthenticated:
		return lua.PermissionDenied
	case connect.CodeResourceExhausted:
		return lua.RateLimited
	case connect.CodeUnavailable:
		return lua.Unavailable
	case connect.CodeDeadlineExceeded:
		return lua.Timeout
	case connect.CodeCanceled:
		return lua.Canceled
	case connect.CodeFailedPrecondition, connect.CodeAborted:
		return lua.Conflict
	case connect.CodeInternal, connect.CodeUnknown:
		return lua.Internal
	default:
		return lua.Unknown
	}
}

func isRetryableConnect(code connect.Code) bool {
	switch code {
	case connect.CodeUnavailable, connect.CodeDeadlineExceeded, connect.CodeResourceExhausted:
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
