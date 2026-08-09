// SPDX-License-Identifier: MPL-2.0

package artifact

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const artifactLockRetry = 25 * time.Millisecond

func acquireArtifactLock(ctx context.Context, root string) (func() error, error) {
	file, err := os.OpenFile(
		filepath.Join(root, ".artifacts.lock"),
		os.O_CREATE|os.O_RDWR,
		0o600,
	)
	if err != nil {
		return nil, fmt.Errorf("open artifact lock: %w", err)
	}

	for {
		unlock, lockErr := tryLockFile(file)
		if lockErr == nil {
			return func() error {
				return errors.Join(unlock(), file.Close())
			}, nil
		}
		if !errors.Is(lockErr, errLockBusy) {
			_ = file.Close()
			return nil, fmt.Errorf("lock artifact root: %w", lockErr)
		}

		timer := time.NewTimer(artifactLockRetry)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			_ = file.Close()
			return nil, fmt.Errorf("lock artifact root: %w", ctx.Err())
		case <-timer.C:
		}
	}
}
