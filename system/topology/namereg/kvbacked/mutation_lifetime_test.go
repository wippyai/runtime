// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"testing"
	"time"
)

func TestStopJoinsAdmittedRemoval(t *testing.T) {
	for _, op := range []string{"unregister", "unregister-strong", "remove", "remove-node", "drop-node"} {
		t.Run(op, func(t *testing.T) {
			r := newStrongReg(t, nil, 0, nil)
			owner := mkPID("node-1", "owner")
			if _, err := r.RegisterScope(context.Background(), "name", owner, globalapi.Consistent); err != nil {
				t.Fatal(err)
			}
			engine := &heldRegistrationTxn{Engine: r.engine, entered: make(chan struct{}), release: make(chan struct{})}
			r.engine = engine
			if err := r.StartReconciler(context.Background()); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() {
				var err error
				switch op {
				case "unregister":
					_, err = r.UnregisterScope(context.Background(), "name", globalapi.Consistent)
				case "unregister-strong":
					_, err = r.UnregisterScope(context.Background(), "name", globalapi.Strong)
				case "remove":
					err = r.Remove(context.Background(), owner)
				case "remove-node":
					err = r.RemoveNode(context.Background(), owner.Node)
				case "drop-node":
					r.DropNode(owner.Node)
				}
				result <- err
			}()
			<-engine.entered
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			if err := r.StopReconciler(ctx); !errors.Is(err, context.DeadlineExceeded) {
				close(engine.release)
				<-result
				_ = r.StopReconciler(context.Background())
				t.Fatalf("shutdown overtook admitted %s: %v", op, err)
			}
			close(engine.release)
			if err := <-result; err != nil {
				t.Fatalf("admitted removal did not finish: %v", err)
			}
			if err := r.StopReconciler(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, err := r.UnregisterScope(context.Background(), "name", globalapi.Consistent); !errors.Is(err, context.Canceled) {
				t.Fatalf("post-stop unregister=%v", err)
			}
			if err := r.Remove(context.Background(), owner); !errors.Is(err, context.Canceled) {
				t.Fatalf("post-stop remove=%v", err)
			}
			if err := r.RemoveNode(context.Background(), owner.Node); !errors.Is(err, context.Canceled) {
				t.Fatalf("post-stop remove-node=%v", err)
			}
		})
	}
}
