// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"encoding/json"
	"time"

	apierror "github.com/wippyai/runtime/api/error"
)

// RestartIntensity caps failure-driven restart admissions in (now-Window, now].
// Initial/manual starts and planned outdated-code restarts do not consume it.
type RestartIntensity struct {
	MaxRestarts int           `json:"max_restarts" yaml:"max_restarts"`
	Window      time.Duration `json:"window" yaml:"window"`
}

func (rp RetryPolicy) Validate() error {
	if rp.Intensity != nil && (rp.Intensity.MaxRestarts <= 0 || rp.Intensity.Window <= 0) {
		return apierror.New(apierror.Invalid, "restart intensity requires positive max_restarts and window").
			WithRetryable(apierror.False)
	}
	return nil
}

func (i *RestartIntensity) UnmarshalJSON(data []byte) error {
	var raw struct {
		Window      string `json:"window"`
		MaxRestarts int    `json:"max_restarts"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	window, err := time.ParseDuration(raw.Window)
	if err != nil {
		return err
	}
	decoded := RestartIntensity{MaxRestarts: raw.MaxRestarts, Window: window}
	if err := (RetryPolicy{Intensity: &decoded}).Validate(); err != nil {
		return err
	}
	*i = decoded
	return nil
}

func (i RestartIntensity) MarshalJSON() ([]byte, error) {
	if err := (RetryPolicy{Intensity: &i}).Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Window      string `json:"window"`
		MaxRestarts int    `json:"max_restarts"`
	}{MaxRestarts: i.MaxRestarts, Window: i.Window.String()})
}
