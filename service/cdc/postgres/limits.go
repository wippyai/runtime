// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	config "github.com/wippyai/runtime/api/service/cdc"
)

const (
	// These aliases keep the decoder package independent from entry parsing
	// while making the finite defaults owned by the public CDC configuration.
	defaultMaxTransactionChanges = config.DefaultPostgresMaxTransactionChanges
	defaultMaxTransactionBytes   = config.DefaultPostgresMaxTransactionBytes
	defaultMaxInflightChanges    = config.DefaultPostgresMaxInflightChanges
	defaultMaxInflightBytes      = config.DefaultPostgresMaxInflightBytes
)

type decoderLimits struct {
	maxChanges         int
	maxBytes           int64
	maxInflightChanges int
	maxInflightBytes   int64
}

func defaultDecoderLimits() decoderLimits {
	return decoderLimits{
		maxChanges:         defaultMaxTransactionChanges,
		maxBytes:           defaultMaxTransactionBytes,
		maxInflightChanges: defaultMaxInflightChanges,
		maxInflightBytes:   defaultMaxInflightBytes,
	}
}

func normalizeDecoderLimits(limits decoderLimits) decoderLimits {
	defaults := defaultDecoderLimits()
	if limits.maxChanges <= 0 {
		limits.maxChanges = defaults.maxChanges
	}
	if limits.maxBytes <= 0 {
		limits.maxBytes = defaults.maxBytes
	}
	if limits.maxInflightChanges <= 0 {
		limits.maxInflightChanges = defaults.maxInflightChanges
	}
	if limits.maxInflightBytes <= 0 {
		limits.maxInflightBytes = defaults.maxInflightBytes
	}
	return limits
}
