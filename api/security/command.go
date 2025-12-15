package security

import (
	"sync"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/dispatcher"
)

func init() {
	dispatcher.MustRegisterCommands("security",
		ValidateToken, CreateToken, RevokeToken,
	)
}

// Command IDs for security operations.
// Range 140-149 is reserved for security/token commands.
const (
	ValidateToken dispatcher.CommandID = 140 // Validate token
	CreateToken   dispatcher.CommandID = 141 // Create token
	RevokeToken   dispatcher.CommandID = 142 // Revoke token
)

// ValidateTokenCmd validates a token.
type ValidateTokenCmd struct {
	TokenStore TokenStore
	Token      Token
}

var validateTokenCmdPool = sync.Pool{New: func() any { return &ValidateTokenCmd{} }}

func AcquireValidateTokenCmd() *ValidateTokenCmd {
	return validateTokenCmdPool.Get().(*ValidateTokenCmd)
}
func (c *ValidateTokenCmd) CmdID() dispatcher.CommandID { return ValidateToken }
func (c *ValidateTokenCmd) Release() {
	c.TokenStore = nil
	c.Token = ""
	validateTokenCmdPool.Put(c)
}

// CreateTokenCmd creates a new token.
type CreateTokenCmd struct {
	TokenStore TokenStore
	Actor      Actor
	Scope      Scope
	Details    TokenDetails
}

var createTokenCmdPool = sync.Pool{New: func() any { return &CreateTokenCmd{} }}

func AcquireCreateTokenCmd() *CreateTokenCmd          { return createTokenCmdPool.Get().(*CreateTokenCmd) }
func (c *CreateTokenCmd) CmdID() dispatcher.CommandID { return CreateToken }
func (c *CreateTokenCmd) Release() {
	c.TokenStore = nil
	c.Actor = Actor{}
	c.Scope = nil
	c.Details = TokenDetails{}
	createTokenCmdPool.Put(c)
}

// RevokeTokenCmd revokes a token.
type RevokeTokenCmd struct {
	TokenStore TokenStore
	Token      Token
}

var revokeTokenCmdPool = sync.Pool{New: func() any { return &RevokeTokenCmd{} }}

func AcquireRevokeTokenCmd() *RevokeTokenCmd          { return revokeTokenCmdPool.Get().(*RevokeTokenCmd) }
func (c *RevokeTokenCmd) CmdID() dispatcher.CommandID { return RevokeToken }
func (c *RevokeTokenCmd) Release() {
	c.TokenStore = nil
	c.Token = ""
	revokeTokenCmdPool.Put(c)
}

// ValidateTokenResponse contains the result of a validate operation.
type ValidateTokenResponse struct {
	Actor Actor
	Scope Scope
	Error error
}

// CreateTokenResponse contains the result of a create operation.
type CreateTokenResponse struct {
	Token Token
	Error error
}

// RevokeTokenResponse contains the result of a revoke operation.
type RevokeTokenResponse struct {
	Error error
}

// NewTokenDetails helper for creating token details.
func NewTokenDetails(expiration time.Duration, meta attrs.Bag) TokenDetails {
	return TokenDetails{
		Expiration: expiration,
		Meta:       meta,
	}
}
