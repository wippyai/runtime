// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/wippyai/runtime/api/supervisor"
)

func validConfig() *Config {
	return &Config{
		Host:        "localhost",
		Port:        5432,
		Username:    "u",
		Password:    "p",
		Database:    "d",
		SlotName:    "wippy_slot",
		Publication: "wippy_pub",
	}
}

func TestConfigValidateOK(t *testing.T) {
	require.NoError(t, validConfig().Validate())
}

func TestConfigValidateTablesInsteadOfPublication(t *testing.T) {
	c := validConfig()
	c.Publication = ""
	c.Tables = []string{"accounts"}
	require.NoError(t, c.Validate())
}

func TestConfigValidateErrors(t *testing.T) {
	cases := map[string]struct {
		mutate func(*Config)
		want   error
	}{
		"missing host":        {func(c *Config) { c.Host = "" }, ErrHostRequired},
		"missing port":        {func(c *Config) { c.Port = 0 }, ErrInvalidPort},
		"missing database":    {func(c *Config) { c.Database = "" }, ErrDatabaseRequired},
		"missing username":    {func(c *Config) { c.Username = "" }, ErrUsernameRequired},
		"missing password":    {func(c *Config) { c.Password = "" }, ErrPasswordRequired},
		"missing slot":        {func(c *Config) { c.SlotName = "" }, ErrSlotNameRequired},
		"missing publication": {func(c *Config) { c.Publication = ""; c.Tables = nil }, ErrPublicationRequired},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := validConfig()
			tc.mutate(c)
			require.ErrorIs(t, c.Validate(), tc.want)
		})
	}
}

func TestConfigEnvSatisfiesRequired(t *testing.T) {
	c := &Config{
		HostEnv:     "H",
		PortEnv:     "P",
		UsernameEnv: "U",
		PasswordEnv: "PW",
		DatabaseEnv: "D",
		SlotName:    "s",
		Publication: "pub",
	}
	require.NoError(t, c.Validate())
}

func TestConfigIntervalsParse(t *testing.T) {
	c := validConfig()
	c.StandbyInterval = "5s"
	c.StatusInterval = "1m"
	require.NoError(t, c.Validate())

	standby, err := c.StandbyDuration()
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, standby)

	status, err := c.StatusDuration()
	require.NoError(t, err)
	assert.Equal(t, time.Minute, status)
}

func TestConfigIntervalsEmptyMeansDefault(t *testing.T) {
	c := validConfig()
	standby, err := c.StandbyDuration()
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), standby)
}

func TestConfigIntervalsInvalid(t *testing.T) {
	for _, bad := range []string{"nonsense", "-5s", "10"} {
		c := validConfig()
		c.StandbyInterval = bad
		require.ErrorIs(t, c.Validate(), ErrInvalidInterval, "standby %q must be rejected", bad)
	}
	c := validConfig()
	c.StatusInterval = "abc"
	require.ErrorIs(t, c.Validate(), ErrInvalidInterval)
}

func TestConfigInitDefaults(t *testing.T) {
	c := &Config{}
	c.InitDefaults()
	assert.Equal(t, DefaultEventSystem, c.EventSystem)
	assert.NotNil(t, c.Options)
	assert.Equal(t, supervisor.StartupRequired, c.Lifecycle.StartupMode())
}
