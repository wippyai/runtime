// SPDX-License-Identifier: MPL-2.0

//go:build race

package xml

// raceSlowdown scales time bounds for the race detector.
const raceSlowdown = 10
