package eventsbus

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWildcard(t *testing.T) {
	w := newWildcard("http.*")
	assert.True(t, w.match("http.SuperEvent"))
	assert.False(t, w.match("https.SuperEvent"))
	//assert.False(t, w.match(""))
	assert.True(t, w.match("*.*"))
	assert.True(t, w.match("http.*"))

	// *.* -> *
	w = newWildcard("*.*")
	assert.True(t, w.match("http.SuperEvent"))
	assert.True(t, w.match("https.SuperEvent"))
	//assert.True(t, w.match(""))
	assert.True(t, w.match("*.*"))
	assert.True(t, w.match("http.*"))

	w = newWildcard("*.ConfigurationUpdated")
	assert.False(t, w.match("http.SuperEvent"))
	assert.False(t, w.match("https.SuperEvent"))
	//assert.False(t, w.match(""))
	assert.True(t, w.match("*.*"))
	assert.True(t, w.match("http.*"))
	assert.True(t, w.match("http.ConfigurationUpdated"))

	w = newWildcard("http.RequestArrived")
	assert.False(t, w.match("http.SuperEvent"))
	assert.False(t, w.match("https.SuperEvent"))
	//assert.False(t, w.match(""))
	assert.True(t, w.match("*.*"))
	assert.True(t, w.match("http.*"))
	assert.True(t, w.match("http.RequestArrived"))
}
