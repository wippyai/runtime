// SPDX-License-Identifier: MPL-2.0

package system

import (
	"os"
	"path/filepath"

	"github.com/wippyai/runtime/api/boot"
)

// defaultDataDirName is the per-user default node data directory.
var defaultDataDirName = []string{".wippy", "store"}

// nodeDataDir resolves the node-level fs data directory shared by the durable
// raft store and durable crdt snapshots. Precedence: the explicit
// cluster.raft.data_dir config value, else ~/.wippy/store. Returns "" only when
// no explicit path is set AND the home directory cannot be determined, in which
// case callers fall back to diskless.
func nodeDataDir(clusterCfg boot.Config) string {
	if clusterCfg != nil {
		if dir := clusterCfg.GetString(ClusterRaftDataDir, ""); dir != "" {
			return dir
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(append([]string{home}, defaultDataDirName...)...)
}
