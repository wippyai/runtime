// SPDX-License-Identifier: MPL-2.0

package xml

const (
	transcoderBufferBytes = 4096
	cp1252ControlCount    = 32
	bitsPerByte           = 8
	startTagReserve       = 64
	maxInternEntries      = 4096
	maxInternBytes        = 1024
	bufferBytes           = 65536
	bufferLookahead       = 8
	maxReferenceBytes     = 32
	defaultMaxDepth       = 256
	defaultMaxAttrs       = 256
	defaultMaxTokenBytes  = 1 << 20
	defaultMaxNodeBytes   = 8 << 20
	defaultMaxNamespaces  = 1024
	cancelCheckInterval   = 64
	referencePeek         = 16
	recentNames           = 64
	asciiSpace            = 0x20
	runeBits              = 32
)
