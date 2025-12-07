package security

import (
	"sync"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/dispatcher"
)

func init() {
	dispatcher.MustRegisterCommands("security",
		CmdTokenValidate, CmdTokenCreate, CmdTokenRevoke,
	)
}

// Command IDs for security operations.
// Range 140-149 is reserved for security/token commands.
const (
	CmdTokenValidate dispatcher.CommandID = 140 // Validate token
	CmdTokenCreate   dispatcher.CommandID = 141 // Create token
	CmdTokenRevoke   dispatcher.CommandID = 142 // Revoke token
)

// TokenValidateCmd validates a token.
type TokenValidateCmd struct {
	TokenStore TokenStore
	Token      Token
}

var tokenValidateCmdPool = sync.Pool{New: func() any { return &TokenValidateCmd{} }}

func AcquireTokenValidateCmd() *TokenValidateCmd {
	return tokenValidateCmdPool.Get().(*TokenValidateCmd)
}
func (c *TokenValidateCmd) CmdID() dispatcher.CommandID { return CmdTokenValidate }
func (c *TokenValidateCmd) Release() {
	c.TokenStore = nil
	c.Token = ""
	tokenValidateCmdPool.Put(c)
}

// TokenCreateCmd creates a new token.
type TokenCreateCmd struct {
	TokenStore TokenStore
	Actor      Actor
	Scope      Scope
	Details    TokenDetails
}

var tokenCreateCmdPool = sync.Pool{New: func() any { return &TokenCreateCmd{} }}

func AcquireTokenCreateCmd() *TokenCreateCmd          { return tokenCreateCmdPool.Get().(*TokenCreateCmd) }
func (c *TokenCreateCmd) CmdID() dispatcher.CommandID { return CmdTokenCreate }
func (c *TokenCreateCmd) Release() {
	c.TokenStore = nil
	c.Actor = Actor{}
	c.Scope = nil
	c.Details = TokenDetails{}
	tokenCreateCmdPool.Put(c)
}

// TokenRevokeCmd revokes a token.
type TokenRevokeCmd struct {
	TokenStore TokenStore
	Token      Token
}

var tokenRevokeCmdPool = sync.Pool{New: func() any { return &TokenRevokeCmd{} }}

func AcquireTokenRevokeCmd() *TokenRevokeCmd          { return tokenRevokeCmdPool.Get().(*TokenRevokeCmd) }
func (c *TokenRevokeCmd) CmdID() dispatcher.CommandID { return CmdTokenRevoke }
func (c *TokenRevokeCmd) Release() {
	c.TokenStore = nil
	c.Token = ""
	tokenRevokeCmdPool.Put(c)
}

// TokenValidateResponse contains the result of a validate operation.
type TokenValidateResponse struct {
	Actor Actor
	Scope Scope
	Error error
}

// TokenCreateResponse contains the result of a create operation.
type TokenCreateResponse struct {
	Token Token
	Error error
}

// TokenRevokeResponse contains the result of a revoke operation.
type TokenRevokeResponse struct {
	Error error
}

// NewTokenDetails helper for creating token details.
func NewTokenDetails(expiration time.Duration, meta attrs.Bag) TokenDetails {
	return TokenDetails{
		Expiration: expiration,
		Meta:       meta,
	}
}
