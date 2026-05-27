// SPDX-License-Identifier: MPL-2.0

package amqp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/attrs"
)

// The "amqp.mandatory" publish header must control the mandatory flag passed
// to PublishWithContext — otherwise a publisher that sets it sees the
// message silently dropped by the broker when no queue is bound. Publish
// must also strip the key from the merged header bag before calling
// buildPublishing, so it never leaks into the AMQP broker headers table.
func TestExtractMandatory_TrueFromHeader(t *testing.T) {
	headers := attrs.NewBag()
	headers.Set("amqp.mandatory", true)

	got := extractMandatory(headers)

	assert.True(t, got, "amqp.mandatory=true must propagate to the publish flag")
	_, still := headers["amqp.mandatory"]
	assert.False(t, still, "extraction must remove the key so it isn't sent as a broker header")
}

func TestExtractMandatory_FalseByDefault(t *testing.T) {
	headers := attrs.NewBag()
	got := extractMandatory(headers)
	assert.False(t, got)
}

func TestExtractMandatory_AcceptsStringTrue(t *testing.T) {
	headers := attrs.NewBag()
	headers.Set("amqp.mandatory", "true")

	got := extractMandatory(headers)

	assert.True(t, got, "string 'true' from YAML/Lua must be honored")
}

// buildPublishing must not leak "amqp.mandatory" into Publishing.Headers if
// the caller forgot to pre-extract — belt-and-suspenders. The canonical flow
// extracts first, but a defensive skip here prevents a round-trip surprise.
func TestBuildPublishing_SkipsMandatoryKey(t *testing.T) {
	headers := attrs.NewBag()
	headers.Set("amqp.mandatory", true)

	pub := buildPublishing("id", []byte("x"), "application/json", headers)

	_, present := pub.Headers["mandatory"]
	assert.False(t, present, "amqp.mandatory must not round-trip as a broker header named 'mandatory'")
}
