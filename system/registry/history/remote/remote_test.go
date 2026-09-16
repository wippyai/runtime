package remote

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	historyv1 "github.com/wippyai/runtime/api/registry/history/v1"
	"github.com/wippyai/runtime/internal/version"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

type testServer struct {
	historyv1.UnimplementedHistoryServiceServer
	submits []*historyv1.SubmitRequest
	mu      sync.Mutex
	lost    bool
}

func (s *testServer) GetCandidate(context.Context, *historyv1.GetRequest) (*historyv1.Candidate, error) {
	return nil, status.Error(codes.NotFound, "empty registry")
}
func (s *testServer) Submit(_ context.Context, req *historyv1.SubmitRequest) (*historyv1.SubmitResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.submits = append(s.submits, req)
	if s.lost {
		s.lost = false
		return nil, status.Error(codes.Unavailable, "response lost")
	}
	return &historyv1.SubmitResponse{Receipt: &historyv1.Receipt{RequestId: req.RequestId, Revision: 1, Status: "stored"}}, nil
}
func (s *testServer) GetReceipt(_ context.Context, req *historyv1.GetReceiptRequest) (*historyv1.Receipt, error) {
	return &historyv1.Receipt{RequestId: req.RequestId, Revision: 1, PublishedRevision: 3, Status: "published"}, nil
}
func (s *testServer) GetVersion(context.Context, *historyv1.GetRequest) (*historyv1.Version, error) {
	return &historyv1.Version{Revision: 3}, nil
}
func (s *testServer) ReportApplied(context.Context, *historyv1.AppliedRequest) (*historyv1.Empty, error) {
	return &historyv1.Empty{}, nil
}

func newTestHistory(t testing.TB, server historyv1.HistoryServiceServer) *History {
	t.Helper()
	return newTestHistoryWithReplica(t, server, "replica")
}

func newTestHistoryWithReplica(t testing.TB, server historyv1.HistoryServiceServer, replica string) *History {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	historyv1.RegisterHistoryServiceServer(grpcServer, server)
	go grpcServer.Serve(listener)
	t.Cleanup(grpcServer.Stop)
	connection, err := grpc.NewClient("passthrough:///history", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	require.NoError(t, err)
	t.Cleanup(func() { connection.Close() })
	history, err := New(connection, Config{Key: &historyv1.RegistryKey{TenantId: "tenant", EnvironmentId: "stage", RegistryId: "registry"}, ReplicaID: replica, Timeout: time.Second, PollInterval: time.Millisecond})
	require.NoError(t, err)
	return history
}

type replicaReportServer struct {
	replicas chan string
	testServer
}

func (s *replicaReportServer) ReportApplied(_ context.Context, request *historyv1.AppliedRequest) (*historyv1.Empty, error) {
	s.replicas <- request.ReplicaId
	return &historyv1.Empty{}, nil
}

func TestAutomaticReplicaReportsRemainStableAndDistinct(t *testing.T) {
	server := &replicaReportServer{replicas: make(chan string, 4)}
	first := newTestHistoryWithReplica(t, server, "")
	second := newTestHistoryWithReplica(t, server, "")
	explicit := newTestHistoryWithReplica(t, server, "operator-replica")
	for _, history := range []*History{first, second, explicit} {
		require.NoError(t, history.ReportApplied(t.Context(), &registry.PublishedState{Version: version.New(1)}, nil))
	}
	require.NoError(t, first.ReportApplied(t.Context(), &registry.PublishedState{Version: version.New(2)}, nil))
	firstID, secondID, explicitID, repeatedID := <-server.replicas, <-server.replicas, <-server.replicas, <-server.replicas
	_, err := uuid.Parse(firstID)
	require.NoError(t, err)
	_, err = uuid.Parse(secondID)
	require.NoError(t, err)
	require.NotEqual(t, firstID, secondID)
	require.Equal(t, firstID, repeatedID)
	require.Equal(t, "operator-replica", explicitID)
}

func TestLostResponseUsesOriginalReceipt(t *testing.T) {
	server := &testServer{lost: true}
	history := newTestHistory(t, server)
	receipt, err := history.SubmitChanges(context.Background(), registry.ChangeSet{{Kind: registry.EntryDelete, Entry: registry.Entry{ID: registry.NewID("test", "entry")}}}, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(1), receipt.Revision)
	published, err := history.AwaitPublished(context.Background(), receipt)
	require.NoError(t, err)
	require.Equal(t, uint(3), published.Version.ID())
	server.mu.Lock()
	defer server.mu.Unlock()
	require.Len(t, server.submits, 1)
	require.Equal(t, server.submits[0].RequestId, receipt.RequestID)
}

func TestConfirmedWriteAdvancesExpectedRevision(t *testing.T) {
	server := &testServer{}
	history := newTestHistory(t, server)
	changes := registry.ChangeSet{{Kind: registry.EntryDelete, Entry: registry.Entry{ID: registry.NewID("test", "entry")}}}
	_, err := history.SubmitChanges(context.Background(), changes, nil)
	require.NoError(t, err)
	_, err = history.SubmitChanges(context.Background(), changes, nil)
	require.NoError(t, err)
	server.mu.Lock()
	defer server.mu.Unlock()
	require.Equal(t, uint64(0), server.submits[0].GetExpectedRevision())
	require.NotNil(t, server.submits[0].ExpectedRevision)
	require.Equal(t, uint64(1), server.submits[1].GetExpectedRevision())
	require.NotNil(t, server.submits[1].ExpectedRevision)
}

func TestReadDoesNotAdvanceUntilVersionIsApplied(t *testing.T) {
	server := &testServer{}
	history := newTestHistory(t, server)
	published, err := history.ReadPublished(context.Background(), 0)
	require.NoError(t, err)
	changes := registry.ChangeSet{{Kind: registry.EntryDelete, Entry: registry.Entry{ID: registry.NewID("test", "entry")}}}
	_, err = history.SubmitChanges(context.Background(), changes, nil)
	require.NoError(t, err)
	require.NoError(t, history.ReportApplied(context.Background(), published, nil))
	_, err = history.SubmitChanges(context.Background(), changes, nil)
	require.NoError(t, err)
	server.mu.Lock()
	defer server.mu.Unlock()
	require.Equal(t, uint64(0), server.submits[0].GetExpectedRevision())
	require.Equal(t, uint64(3), server.submits[1].GetExpectedRevision())
}

func TestFailedApplicationDoesNotAdvanceExpectedRevision(t *testing.T) {
	server := &testServer{}
	history := newTestHistory(t, server)
	published, err := history.ReadPublished(context.Background(), 0)
	require.NoError(t, err)
	require.NoError(t, history.ReportApplied(context.Background(), published, fmt.Errorf("apply failed")))
	changes := registry.ChangeSet{{Kind: registry.EntryDelete, Entry: registry.Entry{ID: registry.NewID("test", "entry")}}}
	_, err = history.SubmitChanges(context.Background(), changes, nil)
	require.NoError(t, err)
	server.mu.Lock()
	defer server.mu.Unlock()
	require.Equal(t, uint64(0), server.submits[0].GetExpectedRevision())
}

func TestLegacyWritesFailExplicitly(t *testing.T) {
	history := newTestHistory(t, &testServer{})
	require.ErrorIs(t, history.Save(nil, nil, true), registry.ErrHistoryOperationUnsupported)
	require.ErrorIs(t, history.SetHead(nil), registry.ErrHistoryOperationUnsupported)
}

type unavailableReceiptServer struct {
	*testServer
}

func (s *unavailableReceiptServer) GetReceipt(context.Context, *historyv1.GetReceiptRequest) (*historyv1.Receipt, error) {
	return nil, status.Error(codes.Unavailable, "receipt unavailable")
}

func TestUnknownCommitRetainsRequestIdentity(t *testing.T) {
	server := &unavailableReceiptServer{testServer: &testServer{lost: true}}
	history := newTestHistory(t, server)
	changes := registry.ChangeSet{{Kind: registry.EntryDelete, Entry: registry.Entry{ID: registry.NewID("test", "entry")}}}
	_, err := history.SubmitChanges(context.Background(), changes, nil)
	require.ErrorIs(t, err, ErrCommitUnknown)
	server.mu.Lock()
	pending := proto.Clone(server.submits[0]).(*historyv1.SubmitRequest)
	server.mu.Unlock()
	require.NoError(t, history.ReportApplied(context.Background(), &registry.PublishedState{Version: version.New(3)}, nil))
	_, err = history.SubmitChanges(context.Background(), registry.ChangeSet{{Kind: registry.EntryDelete, Entry: registry.Entry{ID: registry.NewID("test", "different")}}}, nil)
	require.ErrorIs(t, err, ErrCommitUnknown)
	_, err = history.SubmitChanges(context.Background(), changes, nil)
	require.NoError(t, err)
	server.mu.Lock()
	defer server.mu.Unlock()
	require.Len(t, server.submits, 2)
	require.Equal(t, pending, server.submits[1])
	require.Equal(t, uint64(0), server.submits[1].GetExpectedRevision())
	require.NotNil(t, server.submits[1].ExpectedRevision)
}

type recoveredReceiptServer struct {
	*testServer
	receiptReads int
}

func (s *recoveredReceiptServer) GetReceipt(_ context.Context, request *historyv1.GetReceiptRequest) (*historyv1.Receipt, error) {
	s.receiptReads++
	if s.receiptReads == 1 {
		return nil, status.Error(codes.Unavailable, "receipt unavailable")
	}
	return &historyv1.Receipt{RequestId: request.RequestId, Revision: 1, PublishedRevision: 3, Status: "published"}, nil
}

func TestAppliedPublicationReconcilesUnknownCommit(t *testing.T) {
	server := &recoveredReceiptServer{testServer: &testServer{lost: true}}
	history := newTestHistory(t, server)
	changes := registry.ChangeSet{{Kind: registry.EntryDelete, Entry: registry.Entry{ID: registry.NewID("test", "entry")}}}
	_, err := history.SubmitChanges(t.Context(), changes, nil)
	require.ErrorIs(t, err, ErrCommitUnknown)
	server.mu.Lock()
	pending := proto.Clone(server.submits[0]).(*historyv1.SubmitRequest)
	server.mu.Unlock()
	require.NoError(t, history.ReportApplied(t.Context(), &registry.PublishedState{Version: version.New(3)}, nil))
	_, err = history.SubmitChanges(t.Context(), registry.ChangeSet{{Kind: registry.EntryDelete, Entry: registry.Entry{ID: registry.NewID("test", "next")}}}, nil)
	require.NoError(t, err)
	server.mu.Lock()
	defer server.mu.Unlock()
	require.Equal(t, 2, server.receiptReads)
	require.Len(t, server.submits, 2)
	require.Equal(t, pending, server.submits[0])
	require.NotEqual(t, pending.RequestId, server.submits[1].RequestId)
	require.Equal(t, uint64(3), server.submits[1].GetExpectedRevision())
}

func TestConfirmedStoredRevisionDoesNotProvePendingPublication(t *testing.T) {
	server := &recoveredReceiptServer{testServer: &testServer{}, receiptReads: 1}
	history := newTestHistory(t, server)
	expectedRevision := uint64(0)
	history.revision = 3
	history.pending = &historyv1.SubmitRequest{Key: history.key, RequestId: "pending", ExpectedRevision: &expectedRevision, Mutations: []*historyv1.Mutation{{EntryId: "test:entry", Deleted: true}}}
	pending := proto.Clone(history.pending).(*historyv1.SubmitRequest)
	_, err := history.SubmitChanges(t.Context(), registry.ChangeSet{{Kind: registry.EntryDelete, Entry: registry.Entry{ID: registry.NewID("test", "different")}}}, nil)
	require.ErrorIs(t, err, ErrCommitUnknown)
	require.Equal(t, 1, server.receiptReads)
	require.True(t, proto.Equal(pending, history.pending))
}

type staleServer struct {
	*testServer
	receiptReads int
}

func (s *staleServer) Submit(_ context.Context, request *historyv1.SubmitRequest) (*historyv1.SubmitResponse, error) {
	s.submits = append(s.submits, request)
	return nil, status.Error(codes.Aborted, "revision changed")
}

func (s *staleServer) GetReceipt(context.Context, *historyv1.GetReceiptRequest) (*historyv1.Receipt, error) {
	s.receiptReads++
	return nil, status.Error(codes.NotFound, "request not found")
}

func TestStaleSubmitReturnsConflictWithoutRetry(t *testing.T) {
	server := &staleServer{testServer: &testServer{}}
	history := newTestHistory(t, server)
	changes := registry.ChangeSet{{Kind: registry.EntryDelete, Entry: registry.Entry{ID: registry.NewID("test", "entry")}}}
	_, err := history.SubmitChanges(t.Context(), changes, nil)
	require.ErrorIs(t, err, registry.ErrHistoryConflict)
	require.Equal(t, codes.Aborted, status.Code(err))
	require.Len(t, server.submits, 1)
	require.Zero(t, server.receiptReads)
}

type watchServer struct {
	historyv1.UnimplementedHistoryServiceServer
	cursors []uint64
	mu      sync.Mutex
}

func (s *watchServer) Watch(req *historyv1.ReadRequest, stream grpc.ServerStreamingServer[historyv1.Version]) error {
	s.mu.Lock()
	s.cursors = append(s.cursors, req.AfterRevision)
	revision := uint64(3)
	if len(s.cursors) > 1 {
		revision = 4
	}
	s.mu.Unlock()
	if err := stream.Send(&historyv1.Version{Revision: revision}); err != nil {
		return err
	}
	return status.Error(codes.Unavailable, "stream lost")
}

func TestWatchResumesAfterAppliedCursor(t *testing.T) {
	server := &watchServer{}
	history := newTestHistory(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var revisions []uint
	err := history.FollowPublished(ctx, 0, func(published *registry.PublishedState) error {
		revisions = append(revisions, published.Version.ID())
		if published.Version.ID() == 4 {
			cancel()
		}
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, []uint{3, 4}, revisions)
	server.mu.Lock()
	defer server.mu.Unlock()
	require.Equal(t, []uint64{0, 3}, server.cursors)
}

func TestCanceledPublicationReturnsRequestIdentity(t *testing.T) {
	history := newTestHistory(t, &testServer{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := history.AwaitPublished(ctx, &registry.HistoryReceipt{RequestID: "request", Status: "stored"})
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorContains(t, err, "request")
}

type restoreServer struct {
	*unavailableReceiptServer
	requests []*historyv1.RestoreRequest
}

func (s *restoreServer) Restore(_ context.Context, req *historyv1.RestoreRequest) (*historyv1.SubmitResponse, error) {
	s.requests = append(s.requests, req)
	if len(s.requests) == 1 {
		return nil, status.Error(codes.Unavailable, "restore response lost")
	}
	return &historyv1.SubmitResponse{Receipt: &historyv1.Receipt{RequestId: req.RequestId, Revision: 2, Status: "stored"}}, nil
}
func TestUnknownRestoreRetainsRequestIdentity(t *testing.T) {
	server := &restoreServer{unavailableReceiptServer: &unavailableReceiptServer{testServer: &testServer{}}}
	history := newTestHistory(t, server)
	_, err := history.RestoreChanges(context.Background(), 1)
	require.ErrorIs(t, err, ErrCommitUnknown)
	pending := proto.Clone(server.requests[0]).(*historyv1.RestoreRequest)
	require.NoError(t, history.ReportApplied(context.Background(), &registry.PublishedState{Version: version.New(3)}, nil))
	_, err = history.RestoreChanges(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, server.requests, 2)
	require.Equal(t, pending, server.requests[1])
	require.Equal(t, uint64(0), server.requests[1].GetExpectedRevision())
	require.NotNil(t, server.requests[1].ExpectedRevision)
}

type confirmedRestoreServer struct {
	*testServer
	requests []*historyv1.RestoreRequest
}

func (s *confirmedRestoreServer) Restore(_ context.Context, request *historyv1.RestoreRequest) (*historyv1.SubmitResponse, error) {
	s.requests = append(s.requests, request)
	return &historyv1.SubmitResponse{Receipt: &historyv1.Receipt{RequestId: request.RequestId, Revision: 2, Status: "stored"}}, nil
}

func TestConfirmedRestoreAdvancesExpectedRevision(t *testing.T) {
	server := &confirmedRestoreServer{testServer: &testServer{}}
	history := newTestHistory(t, server)
	_, err := history.RestoreChanges(t.Context(), 1)
	require.NoError(t, err)
	changes := registry.ChangeSet{{Kind: registry.EntryDelete, Entry: registry.Entry{ID: registry.NewID("test", "entry")}}}
	_, err = history.SubmitChanges(t.Context(), changes, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(0), server.requests[0].GetExpectedRevision())
	server.mu.Lock()
	defer server.mu.Unlock()
	require.Equal(t, uint64(2), server.submits[0].GetExpectedRevision())
}

type staleRestoreServer struct {
	*testServer
	requests     []*historyv1.RestoreRequest
	receiptReads int
}

func (s *staleRestoreServer) Restore(_ context.Context, request *historyv1.RestoreRequest) (*historyv1.SubmitResponse, error) {
	s.requests = append(s.requests, request)
	return nil, status.Error(codes.Aborted, "revision changed")
}

func (s *staleRestoreServer) GetReceipt(context.Context, *historyv1.GetReceiptRequest) (*historyv1.Receipt, error) {
	s.receiptReads++
	return nil, status.Error(codes.NotFound, "request not found")
}

func TestStaleRestoreReturnsConflictWithoutRetry(t *testing.T) {
	server := &staleRestoreServer{testServer: &testServer{}}
	history := newTestHistory(t, server)
	_, err := history.RestoreChanges(t.Context(), 1)
	require.ErrorIs(t, err, registry.ErrHistoryConflict)
	require.Equal(t, codes.Aborted, status.Code(err))
	require.Len(t, server.requests, 1)
	require.Zero(t, server.receiptReads)
}

type reportRetryServer struct {
	*testServer
	reports int
}

func (s *reportRetryServer) ReportApplied(context.Context, *historyv1.AppliedRequest) (*historyv1.Empty, error) {
	s.reports++
	if s.reports == 1 {
		return nil, status.Error(codes.Unavailable, "report response lost")
	}
	return &historyv1.Empty{}, nil
}
func TestFollowRetriesAppliedReportBeforeAdvancing(t *testing.T) {
	server := &reportRetryServer{testServer: &testServer{}}
	history := newTestHistory(t, server)
	published, err := history.ReadPublished(context.Background(), 0)
	require.NoError(t, err)
	require.Error(t, history.ReportApplied(context.Background(), published, nil))
	changes := registry.ChangeSet{{Kind: registry.EntryDelete, Entry: registry.Entry{ID: registry.NewID("test", "entry")}}}
	_, err = history.SubmitChanges(t.Context(), changes, nil)
	require.NoError(t, err)
	server.mu.Lock()
	require.Equal(t, uint64(3), server.submits[0].GetExpectedRevision())
	server.mu.Unlock()
	err = history.FollowPublished(context.Background(), 3, func(*registry.PublishedState) error { return nil })
	require.Equal(t, codes.Unimplemented, status.Code(err))
	require.Equal(t, 2, server.reports)
}

func TestSubmitRetainsFinalValueForEntryReplacement(t *testing.T) {
	server := &testServer{}
	history := newTestHistory(t, server)
	id := registry.NewID("test", "entry")
	_, err := history.SubmitChanges(t.Context(), registry.ChangeSet{{Kind: registry.EntryDelete, Entry: registry.Entry{ID: id, Kind: "old"}}, {Kind: registry.EntryCreate, Entry: registry.Entry{ID: id, Kind: "new"}}}, nil)
	require.NoError(t, err)
	server.mu.Lock()
	defer server.mu.Unlock()
	require.Len(t, server.submits, 1)
	require.Len(t, server.submits[0].Mutations, 1)
	require.Equal(t, id.String(), server.submits[0].Mutations[0].EntryId)
	require.False(t, server.submits[0].Mutations[0].Deleted)
	require.NotEmpty(t, server.submits[0].Mutations[0].Value)
}

type monotonicReportServer struct {
	historyv1.UnimplementedHistoryServiceServer
	revision uint64
	lose     bool
}

func (s *monotonicReportServer) ReportApplied(_ context.Context, request *historyv1.AppliedRequest) (*historyv1.Empty, error) {
	if request.Revision < s.revision {
		return nil, status.Error(codes.Aborted, "causal state changed")
	}
	s.revision = request.Revision
	if s.lose {
		s.lose = false
		return nil, status.Error(codes.Unavailable, "response lost")
	}
	return &historyv1.Empty{}, nil
}

func TestAppliedReportsDoNotMoveBackwards(t *testing.T) {
	for _, lose := range []bool{false, true} {
		t.Run(fmt.Sprint(lose), func(t *testing.T) {
			server := &monotonicReportServer{lose: lose}
			history := newTestHistory(t, server)
			err := history.ReportApplied(t.Context(), &registry.PublishedState{Version: version.New(3)}, nil)
			if lose {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, history.ReportApplied(t.Context(), &registry.PublishedState{Version: version.New(2)}, nil))
			err = history.FollowPublished(t.Context(), 3, func(*registry.PublishedState) error { return nil })
			require.Equal(t, codes.Unimplemented, status.Code(err))
		})
	}
}

func BenchmarkRemoteStoredVersion(b *testing.B) {
	history := newTestHistory(b, &testServer{})
	b.ReportAllocs()
	for b.Loop() {
		if _, err := history.readVersion(b.Context(), 3, true); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRemoteAppliedReport(b *testing.B) {
	history := newTestHistory(b, &testServer{})
	published := &registry.PublishedState{Version: version.New(3)}
	b.ReportAllocs()
	for b.Loop() {
		if err := history.ReportApplied(b.Context(), published, nil); err != nil {
			b.Fatal(err)
		}
	}
}
