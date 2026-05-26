// SPDX-License-Identifier: MPL-2.0

package workflow

import (
	clockapi "github.com/wippyai/runtime/api/clock"
)

// executeSleep handles time.sleep command - blocks for a duration.
func (d *Definition) executeSleep(cmd clockapi.SleepCmd, tag uint64) error {
	d.timers.Sleep(cmd.Duration, func(data any, err error) {
		d.enqueueYieldCompletion(tag, data, err)
	})
	return nil
}

// executeTimerStart handles time.after/time.timer - creates a one-shot timer.
func (d *Definition) executeTimerStart(cmd clockapi.TimerStartCmd, tag uint64) error {
	d.timers.StartTimer(cmd, func(data any, err error) {
		d.resumeProcess(tag, data, err)
	})
	return nil
}

// executeTimerStop cancels an active timer.
func (d *Definition) executeTimerStop(cmd clockapi.TimerStopCmd, tag uint64) error {
	d.timers.StopTimer(cmd.TimerID, func(data any, err error) {
		d.resumeProcess(tag, data, err)
	})
	return nil
}

// executeTimerReset resets a timer with a new duration.
func (d *Definition) executeTimerReset(cmd clockapi.TimerResetCmd, tag uint64) error {
	d.timers.ResetTimer(cmd.TimerID, cmd.Duration, func(data any, err error) {
		d.resumeProcess(tag, data, err)
	})
	return nil
}

// executeTickerStart creates a repeating ticker.
func (d *Definition) executeTickerStart(cmd clockapi.TickerStartCmd, tag uint64) error {
	d.timers.StartTicker(cmd, func(data any, err error) {
		d.resumeProcess(tag, data, err)
	})
	return nil
}

// executeTickerStop stops a running ticker.
func (d *Definition) executeTickerStop(cmd clockapi.TickerStopCmd, tag uint64) error {
	d.timers.StopTicker(cmd.TickerID, func(data any, err error) {
		d.resumeProcess(tag, data, err)
	})
	return nil
}

// executeTimerStopByChID cancels a router-tagged timer by (epoch, chID).
func (d *Definition) executeTimerStopByChID(cmd clockapi.TimerStopByChIDCmd, tag uint64) error {
	d.timers.StopTimerByChID(cmd.Epoch, cmd.ChID, func(data any, err error) {
		d.resumeProcess(tag, data, err)
	})
	return nil
}

// executeTickerStopByChID cancels a router-tagged ticker by (epoch, chID).
func (d *Definition) executeTickerStopByChID(cmd clockapi.TickerStopByChIDCmd, tag uint64) error {
	d.timers.StopTickerByChID(cmd.Epoch, cmd.ChID, func(data any, err error) {
		d.resumeProcess(tag, data, err)
	})
	return nil
}
