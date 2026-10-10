// SPDX-License-Identifier: MPL-2.0

// Package remotetest provides an in-process History service for tests. It
// stores each registry in the memory history driver.
package remotetest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/wippyai/runtime/api/registry"
	historyv1 "github.com/wippyai/runtime/api/registry/history/v1"
	"github.com/wippyai/runtime/internal/version"
	"github.com/wippyai/runtime/system/registry/history/memory"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type storedRegistry struct {
	history        *memory.Storage
	changesets     map[uint][]byte
	baseline       *historyv1.Baseline
	transferID     string
	transferDigest string
	transferring   bool
}

// Server is an in-process History service.
type Server struct {
	historyv1.UnimplementedHistoryServiceServer
	registries map[string]*storedRegistry
	// Fail returns an error for a method before (after=false) or after
	// (after=true) the service applies it. It returns nil to run normally.
	Fail func(method string, after bool) error
	mu   sync.Mutex
}

// Start serves a new Server and returns a client connection to it.
func Start() (*Server, *grpc.ClientConn, func()) {
	server := &Server{registries: make(map[string]*storedRegistry)}
	listener := bufconn.Listen(1 << 20)
	grpcServer := grpc.NewServer()
	historyv1.RegisterHistoryServiceServer(grpcServer, server)
	go func() { _ = grpcServer.Serve(listener) }()
	connection, err := grpc.NewClient("passthrough:///history", grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return listener.DialContext(ctx)
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		panic(err)
	}
	return server, connection, func() {
		_ = connection.Close()
		grpcServer.Stop()
	}
}

func keyString(key *historyv1.RegistryKey) string {
	return key.GetTenantId() + "/" + key.GetEnvironmentId() + "/" + key.GetRegistryId()
}

func (s *Server) run(method string, key *historyv1.RegistryKey, apply func(*storedRegistry) error) error {
	if s.Fail != nil {
		if err := s.Fail(method, false); err != nil {
			return err
		}
	}
	s.mu.Lock()
	stored, ok := s.registries[keyString(key)]
	if !ok {
		stored = &storedRegistry{history: memory.New(), changesets: map[uint][]byte{0: {0x90}}}
		_ = stored.history.SetHead(version.New(registry.RootVersion))
		s.registries[keyString(key)] = stored
	}
	var err error
	if stored.transferring && !strings.Contains(method, "Transfer") {
		err = status.Error(codes.FailedPrecondition, "history transfer is not complete")
	} else {
		err = apply(stored)
	}
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if s.Fail != nil {
		return s.Fail(method, true)
	}
	return nil
}

func storageError(err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	switch {
	case strings.Contains(message, "not found"):
		return status.Error(codes.NotFound, message)
	case strings.Contains(message, "head changed"):
		return status.Error(codes.Aborted, message)
	case strings.Contains(message, "already exists"):
		return status.Error(codes.AlreadyExists, message)
	}
	return status.Error(codes.FailedPrecondition, message)
}

func lineage(v registry.Version) *historyv1.Lineage {
	ids := make([]uint64, 0)
	for current := v; current != nil && current.ID() != registry.RootVersion; current = current.Previous() {
		ids = append(ids, uint64(current.ID()))
	}
	for left, right := 0, len(ids)-1; left < right; left, right = left+1, right-1 {
		ids[left], ids[right] = ids[right], ids[left]
	}
	return &historyv1.Lineage{Ids: ids}
}

func resolution(data []byte) (*registry.DependencyResolution, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var decoded registry.DependencyResolution
	if err := json.Unmarshal(data, &decoded); err != nil || !decoded.Valid() {
		return nil, status.Error(codes.InvalidArgument, "invalid dependency resolution")
	}
	return decoded.Canonical(), nil
}

var handle = &codec.MsgpackHandle{}

func resolutionDigest(resolution *registry.DependencyResolution) string {
	if resolution == nil {
		return ""
	}
	return resolution.Digest
}

func validChangeSet(data []byte) error {
	var operations []any
	if err := codec.NewDecoderBytes(data, handle).Decode(&operations); err != nil {
		return status.Error(codes.InvalidArgument, "invalid changeset")
	}
	return nil
}

func (s *Server) Head(_ context.Context, request *historyv1.RegistryRequest) (*historyv1.Lineage, error) {
	var result *historyv1.Lineage
	err := s.run("Head", request.GetKey(), func(r *storedRegistry) error {
		head, err := r.history.Head()
		result = lineage(head)
		return storageError(err)
	})
	return result, err
}

func (s *Server) GetVersion(_ context.Context, request *historyv1.VersionRequest) (*historyv1.Lineage, error) {
	var result *historyv1.Lineage
	err := s.run("GetVersion", request.GetKey(), func(r *storedRegistry) error {
		stored, err := r.history.GetVersion(uint(request.GetVersionId()))
		if err == nil {
			result = lineage(stored)
		}
		return storageError(err)
	})
	return result, err
}

func (s *Server) GetMaxVersionID(_ context.Context, request *historyv1.RegistryRequest) (*historyv1.MaxVersionID, error) {
	var result *historyv1.MaxVersionID
	err := s.run("GetMaxVersionID", request.GetKey(), func(r *storedRegistry) error {
		id, err := r.history.MaxVersionID()
		result = &historyv1.MaxVersionID{VersionId: uint64(id)}
		return storageError(err)
	})
	return result, err
}

func (s *Server) ListVersions(_ context.Context, request *historyv1.ListVersionsRequest) (*historyv1.VersionList, error) {
	result := &historyv1.VersionList{}
	err := s.run("ListVersions", request.GetKey(), func(r *storedRegistry) error {
		versions, err := r.history.Versions()
		if err != nil {
			return storageError(err)
		}
		limit := int(request.GetLimit())
		for _, stored := range versions {
			if request.AfterId != nil && uint64(stored.ID()) <= request.GetAfterId() {
				continue
			}
			if len(result.Versions) == limit {
				result.HasMore = true
				break
			}
			node := &historyv1.VersionNode{Id: uint64(stored.ID())}
			if previous := stored.Previous(); previous != nil {
				parent := uint64(previous.ID())
				node.ParentId = &parent
			}
			result.Versions = append(result.Versions, node)
		}
		return nil
	})
	return result, err
}

func (s *Server) GetChangeSet(_ context.Context, request *historyv1.VersionRequest) (*historyv1.ChangeSet, error) {
	var result *historyv1.ChangeSet
	err := s.run("GetChangeSet", request.GetKey(), func(r *storedRegistry) error {
		data, ok := r.changesets[uint(request.GetVersionId())]
		if !ok {
			return status.Errorf(codes.NotFound, "version not found: %d", request.GetVersionId())
		}
		result = &historyv1.ChangeSet{VersionId: request.GetVersionId(), Data: data}
		return nil
	})
	return result, err
}

func (s *Server) ReplayChanges(request *historyv1.VersionRequest, stream grpc.ServerStreamingServer[historyv1.ChangeSet]) error {
	var result []*historyv1.ChangeSet
	err := s.run("ReplayChanges", request.GetKey(), func(r *storedRegistry) error {
		target, err := r.history.GetVersion(uint(request.GetVersionId()))
		if err != nil {
			return storageError(err)
		}
		for _, id := range lineage(target).GetIds() {
			result = append(result, &historyv1.ChangeSet{VersionId: id, Data: r.changesets[uint(id)]})
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, item := range result {
		if err := stream.Send(item); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) Save(_ context.Context, request *historyv1.SaveRequest) (*historyv1.Empty, error) {
	err := s.run("Save", request.GetKey(), func(r *storedRegistry) error {
		return r.save(request.GetVersionId(), request.GetParentId(), request.GetChangeset(), request.GetResolution(), request.GetHead(), request.GetRetry())
	})
	return &historyv1.Empty{}, err
}

func (r *storedRegistry) save(id, parentID uint64, data, graph []byte, head, retry bool) error {
	if id <= parentID {
		return status.Error(codes.InvalidArgument, "version must follow its parent")
	}
	if err := validChangeSet(data); err != nil {
		return err
	}
	decoded, err := resolution(graph)
	if err != nil {
		return err
	}
	if existing, ok := r.changesets[uint(id)]; ok && retry {
		stored, err := r.history.GetVersion(uint(id))
		if err == nil && stored.Previous().ID() == uint(parentID) && string(existing) == string(data) {
			if current, err := r.history.Head(); err == nil && (!head || current.ID() == uint(id)) {
				return nil
			}
		}
	}
	parent, err := r.history.GetVersion(uint(parentID))
	if err != nil {
		return storageError(err)
	}
	if err := r.history.SaveWithDependencyResolution(version.FromParent(parent, uint(id)), nil, decoded, head); err != nil {
		return storageError(err)
	}
	r.changesets[uint(id)] = append([]byte(nil), data...)
	return nil
}

func (s *Server) SetHead(_ context.Context, request *historyv1.SetHeadRequest) (*historyv1.Empty, error) {
	err := s.run("SetHead", request.GetKey(), func(r *storedRegistry) error {
		return storageError(r.history.SetHead(version.New(uint(request.GetVersionId()))))
	})
	return &historyv1.Empty{}, err
}

func (s *Server) CompareAndSetHead(_ context.Context, request *historyv1.CompareAndSetHeadRequest) (*historyv1.Empty, error) {
	err := s.run("CompareAndSetHead", request.GetKey(), func(r *storedRegistry) error {
		expected, target := version.New(uint(request.GetExpectedId())), version.New(uint(request.GetTargetId()))
		decoded, err := resolution(request.GetResolution())
		if err != nil {
			return err
		}
		if request.GetRetry() {
			if head, err := r.history.Head(); err == nil && head.ID() == target.ID() {
				stored, err := r.history.GetDependencyResolution(target)
				if decoded == nil || (err == nil && stored.Digest == decoded.Digest) {
					return nil
				}
			}
		}
		if decoded == nil {
			return storageError(r.history.CompareAndSetHead(expected, target))
		}
		return storageError(r.history.CompareAndSetHeadWithDependencyResolution(expected, target, decoded))
	})
	return &historyv1.Empty{}, err
}

func (s *Server) GetResolution(_ context.Context, request *historyv1.VersionRequest) (*historyv1.Resolution, error) {
	var result *historyv1.Resolution
	err := s.run("GetResolution", request.GetKey(), func(r *storedRegistry) error {
		stored, err := r.history.GetDependencyResolution(version.New(uint(request.GetVersionId())))
		if err != nil {
			return status.Error(codes.NotFound, err.Error())
		}
		data, err := json.Marshal(stored)
		result = &historyv1.Resolution{Data: data}
		return err
	})
	return result, err
}

func (s *Server) CheckpointResolution(_ context.Context, request *historyv1.CheckpointResolutionRequest) (*historyv1.Empty, error) {
	err := s.run("CheckpointResolution", request.GetKey(), func(r *storedRegistry) error {
		decoded, err := resolution(request.GetResolution())
		if err != nil {
			return err
		}
		return storageError(r.history.CheckpointDependencyResolution(version.New(uint(request.GetVersionId())), decoded))
	})
	return &historyv1.Empty{}, err
}

func (s *Server) GetBaseline(_ context.Context, request *historyv1.RegistryRequest) (*historyv1.Baseline, error) {
	var result *historyv1.Baseline
	err := s.run("GetBaseline", request.GetKey(), func(r *storedRegistry) error {
		if r.baseline == nil {
			return status.Error(codes.NotFound, "history baseline not found")
		}
		result = r.baseline
		return nil
	})
	return result, err
}

func (s *Server) SetBaseline(_ context.Context, request *historyv1.SetBaselineRequest) (*historyv1.Empty, error) {
	err := s.run("SetBaseline", request.GetKey(), func(r *storedRegistry) error {
		if err := validBaseline(request.GetBaseline()); err != nil {
			return err
		}
		if r.baseline.GetDigest() == request.GetBaseline().GetDigest() {
			return nil
		}
		if r.baseline.GetDigest() != request.GetExpectedDigest() {
			return status.Error(codes.Aborted, "history baseline changed")
		}
		r.baseline = request.GetBaseline()
		return nil
	})
	return &historyv1.Empty{}, err
}

func validBaseline(baseline *historyv1.Baseline) error {
	sum := sha256.Sum256(baseline.GetState())
	if baseline == nil || baseline.GetDigest() != "sha256:"+hex.EncodeToString(sum[:]) {
		return status.Error(codes.InvalidArgument, "invalid history baseline")
	}
	return nil
}

func (s *Server) BeginTransfer(_ context.Context, request *historyv1.BeginTransferRequest) (*historyv1.TransferStatus, error) {
	result := &historyv1.TransferStatus{}
	err := s.run("BeginTransfer", request.GetKey(), func(r *storedRegistry) error {
		if request.GetTransferId() == "" {
			return status.Error(codes.InvalidArgument, "transfer ID is required")
		}
		if r.transferID == request.GetTransferId() {
			result = &historyv1.TransferStatus{Completed: !r.transferring, Digest: r.transferDigest}
			return nil
		}
		maxID, _ := r.history.MaxVersionID()
		if r.transferID != "" || maxID != registry.RootVersion || r.baseline != nil {
			return status.Error(codes.FailedPrecondition, "remote history is not empty")
		}
		r.transferID, r.transferring = request.GetTransferId(), true
		return nil
	})
	return result, err
}

func (s *Server) TransferVersions(_ context.Context, request *historyv1.TransferVersionsRequest) (*historyv1.Empty, error) {
	err := s.run("TransferVersions", request.GetKey(), func(r *storedRegistry) error {
		if !r.transferring || r.transferID != request.GetTransferId() {
			return status.Error(codes.FailedPrecondition, "history transfer is not active")
		}
		for _, item := range request.GetVersions() {
			if existing, ok := r.changesets[uint(item.GetId())]; ok {
				stored, _ := r.history.GetVersion(uint(item.GetId()))
				resolved, _ := r.history.GetDependencyResolution(stored)
				decoded, err := resolution(item.GetResolution())
				if err != nil {
					return err
				}
				if stored.Previous().ID() != uint(item.GetParentId()) || string(existing) != string(item.GetChangeset()) || resolutionDigest(resolved) != resolutionDigest(decoded) {
					return status.Errorf(codes.FailedPrecondition, "transferred version %d differs", item.GetId())
				}
				continue
			}
			if err := r.save(item.GetId(), item.GetParentId(), item.GetChangeset(), item.GetResolution(), false, false); err != nil {
				return err
			}
		}
		return nil
	})
	return &historyv1.Empty{}, err
}

func (s *Server) CompleteTransfer(_ context.Context, request *historyv1.CompleteTransferRequest) (*historyv1.TransferStatus, error) {
	var result *historyv1.TransferStatus
	err := s.run("CompleteTransfer", request.GetKey(), func(r *storedRegistry) error {
		if r.transferID != request.GetTransferId() {
			return status.Error(codes.FailedPrecondition, "history transfer is not active")
		}
		if r.transferring {
			if err := validBaseline(request.GetBaseline()); err != nil {
				return err
			}
			head, err := r.history.GetVersion(uint(request.GetHeadId()))
			if err != nil {
				return storageError(err)
			}
			if err := r.history.SetHead(head); err != nil {
				return storageError(err)
			}
			digest, err := r.digest(request.GetBaseline().GetDigest())
			if err != nil {
				return err
			}
			r.baseline, r.transferDigest, r.transferring = request.GetBaseline(), digest, false
		}
		result = &historyv1.TransferStatus{Completed: true, Digest: r.transferDigest}
		return nil
	})
	return result, err
}

// digest uses the transfer digest format of the History service.
func (r *storedRegistry) digest(baselineDigest string) (string, error) {
	versions, err := r.history.Versions()
	if err != nil {
		return "", err
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].ID() < versions[j].ID() })
	head, err := r.history.Head()
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, _ = fmt.Fprintf(hash, "head %d\nbaseline %s\n", head.ID(), baselineDigest)
	for _, stored := range versions {
		if stored.ID() == registry.RootVersion {
			continue
		}
		var resolutionDigest string
		if resolved, err := r.history.GetDependencyResolution(stored); err == nil {
			resolutionDigest = resolved.Digest
		}
		sum := sha256.Sum256(r.changesets[stored.ID()])
		_, _ = fmt.Fprintf(hash, "%d %d %x %s\n", stored.ID(), stored.Previous().ID(), sum, resolutionDigest)
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}
