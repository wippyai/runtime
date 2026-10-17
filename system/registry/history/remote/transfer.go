// SPDX-License-Identifier: MPL-2.0

package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"sort"

	"github.com/wippyai/runtime/api/registry"
	historyv1 "github.com/wippyai/runtime/api/registry/history/v1"
	"google.golang.org/protobuf/proto"
)

const transferBatchVersions = 500

// Transfer copies the source history and baseline to this remote history. The
// remote history must be empty or hold an earlier attempt with the same
// transferID. The source is not changed. A retry with the same transferID
// resumes the transfer. Transfer returns only after the service confirms the
// same content that it read from the source.
func (h *History) Transfer(ctx context.Context, transferID string, source registry.ResolutionHistory, baseline registry.State) error {
	if transferID == "" {
		return errors.New("history transfer ID is required")
	}
	started, err := call(h, true, func(callCtx context.Context, _ bool) (*historyv1.TransferStatus, error) {
		return h.client.BeginTransfer(callCtx, &historyv1.BeginTransferRequest{Key: h.key, TransferId: transferID})
	})
	if err != nil {
		return fmt.Errorf("begin history transfer: %w", err)
	}

	versions, err := source.Versions()
	if err != nil {
		return fmt.Errorf("read source history versions: %w", err)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].ID() < versions[j].ID() })
	head, err := sourceHead(source, len(versions))
	if err != nil {
		return err
	}
	state, stateDigest, err := encodeState(baseline)
	if err != nil {
		return fmt.Errorf("encode history baseline: %w", err)
	}

	expected := newTransferDigest(head, stateDigest)
	batch := &historyv1.TransferVersionsRequest{Key: h.key, TransferId: transferID}
	flush := func() error {
		if started.GetCompleted() || len(batch.Versions) == 0 {
			return nil
		}
		if _, err := call(h, true, func(callCtx context.Context, _ bool) (*historyv1.Empty, error) {
			return h.client.TransferVersions(mergeContext(callCtx, ctx), batch)
		}); err != nil {
			return fmt.Errorf("transfer history versions: %w", err)
		}
		batch.Versions = batch.Versions[:0]
		return nil
	}
	for _, v := range versions {
		if v.ID() == registry.RootVersion {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		item, resolutionDigest, err := transferVersion(source, v)
		if err != nil {
			return err
		}
		expected.add(item, resolutionDigest)
		if len(batch.Versions) > 0 && (len(batch.Versions) == transferBatchVersions || proto.Size(batch)+proto.Size(item) > h.maxMessageBytes/2) {
			if err := flush(); err != nil {
				return err
			}
		}
		batch.Versions = append(batch.Versions, item)
	}
	if err := flush(); err != nil {
		return err
	}

	confirmed := started
	if !started.GetCompleted() {
		confirmed, err = call(h, true, func(callCtx context.Context, _ bool) (*historyv1.TransferStatus, error) {
			return h.client.CompleteTransfer(callCtx, &historyv1.CompleteTransferRequest{Key: h.key, TransferId: transferID, HeadId: uint64(head), Baseline: &historyv1.Baseline{State: state, Digest: stateDigest}})
		})
		if err != nil {
			return fmt.Errorf("complete history transfer: %w", err)
		}
	}
	if confirmed.GetDigest() != expected.sum() {
		return errors.New("remote history differs from the transferred source history")
	}
	return nil
}

func sourceHead(source registry.History, versions int) (uint, error) {
	head, err := source.Head()
	if err != nil {
		if versions <= 1 {
			return registry.RootVersion, nil
		}
		return 0, fmt.Errorf("read source history head: %w", err)
	}
	return head.ID(), nil
}

func transferVersion(source registry.ResolutionHistory, v registry.Version) (*historyv1.TransferVersion, string, error) {
	if v.Previous() == nil {
		return nil, "", fmt.Errorf("source history version %d has no parent", v.ID())
	}
	if v.Previous().ID() >= v.ID() {
		return nil, "", fmt.Errorf("source history version %d does not follow its parent", v.ID())
	}
	changes, err := source.Get(v)
	if err != nil {
		return nil, "", fmt.Errorf("read source history version %d: %w", v.ID(), err)
	}
	data, err := encodeChangeSet(changes)
	if err != nil {
		return nil, "", fmt.Errorf("encode source history version %d: %w", v.ID(), err)
	}
	item := &historyv1.TransferVersion{Id: uint64(v.ID()), ParentId: uint64(v.Previous().ID()), Changeset: data}
	resolution, err := source.GetDependencyResolution(v)
	switch {
	case errors.Is(err, registry.ErrDependencyResolutionNotFound):
		return item, "", nil
	case err != nil:
		return nil, "", fmt.Errorf("read source dependency resolution of version %d: %w", v.ID(), err)
	}
	if item.Resolution, err = encodeResolution(resolution); err != nil {
		return nil, "", err
	}
	return item, resolution.Canonical().Digest, nil
}

// transferDigest has the same format as the digest that the History service
// returns from CompleteTransfer.
type transferDigest struct{ hash hash.Hash }

func newTransferDigest(head uint, baselineDigest string) *transferDigest {
	digest := &transferDigest{hash: sha256.New()}
	_, _ = fmt.Fprintf(digest.hash, "head %d\nbaseline %s\n", head, baselineDigest)
	return digest
}

func (d *transferDigest) add(item *historyv1.TransferVersion, resolutionDigest string) {
	sum := sha256.Sum256(item.GetChangeset())
	_, _ = fmt.Fprintf(d.hash, "%d %d %x %s\n", item.GetId(), item.GetParentId(), sum, resolutionDigest)
}

func (d *transferDigest) sum() string {
	return "sha256:" + hex.EncodeToString(d.hash.Sum(nil))
}
