// SPDX-License-Identifier: MPL-2.0

//go:build windows

// Package windows contains the Windows-native enforcement primitives used by
// exec confinement. The public policy algebra remains platform-neutral.
package windows

import (
	"errors"
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const bytesPerMiB = 1024 * 1024

// Job is an unnamed, non-inheritable process domain. Closing it kills every
// process still assigned to it; breakaway is never enabled.
type Job struct {
	handle windows.Handle
}

type basicAccountingInformation struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

func NewJob(memoryMiB int64) (*Job, error) {
	handle, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	job := &Job{handle: handle}
	var limits windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE |
		windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS
	limits.BasicLimitInformation.ActiveProcessLimit = 1
	if memoryMiB > 0 {
		if uint64(memoryMiB) > uint64(^uintptr(0))/bytesPerMiB {
			_ = job.Close()
			return nil, errors.New("memory limit overflows native job size")
		}
		limits.BasicLimitInformation.LimitFlags |= windows.JOB_OBJECT_LIMIT_JOB_MEMORY
		limits.JobMemoryLimit = uintptr(memoryMiB) * bytesPerMiB
	}
	if _, err := windows.SetInformationJobObject(handle, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		_ = job.Close()
		return nil, err
	}
	return job, nil
}

// AddSuspended assigns a process before any payload instruction is resumed.
func (j *Job) AddSuspended(pid int) error {
	if j == nil || j.handle == 0 || pid <= 0 {
		return errors.New("invalid job or process identity")
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|
		windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	return windows.AssignProcessToJobObject(j.handle, process)
}

// ResumeMainThread finds the sole thread of a newly-created suspended process
// and resumes it. The caller must assign the process to its Job first.
func ResumeMainThread(pid int) error {
	if pid <= 0 {
		return errors.New("invalid suspended process identity")
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	err = windows.Thread32First(snapshot, &entry)
	for err == nil {
		if entry.OwnerProcessID == uint32(pid) {
			thread, openErr := windows.OpenThread(windows.THREAD_SUSPEND_RESUME|windows.THREAD_QUERY_LIMITED_INFORMATION,
				false, entry.ThreadID)
			if openErr != nil {
				return openErr
			}
			_, resumeErr := windows.ResumeThread(thread)
			_ = windows.CloseHandle(thread)
			return resumeErr
		}
		err = windows.Thread32Next(snapshot, &entry)
	}
	return fmt.Errorf("find suspended process thread: %w", err)
}

func (j *Job) Kill(exitCode uint32) error {
	if j == nil || j.handle == 0 {
		return nil
	}
	return windows.TerminateJobObject(j.handle, exitCode)
}

func (j *Job) verifySingleton() error {
	if j == nil || j.handle == 0 {
		return errors.New("invalid confinement Job")
	}
	var limits windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	if err := windows.QueryInformationJobObject(j.handle, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits)), nil); err != nil {
		return err
	}
	flags := limits.BasicLimitInformation.LimitFlags
	required := uint32(windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS)
	if flags&required != required || limits.BasicLimitInformation.ActiveProcessLimit != 1 {
		return fmt.Errorf("Job singleton limits are not active: flags=%#x processes=%d",
			flags, limits.BasicLimitInformation.ActiveProcessLimit)
	}
	breakaway := uint32(windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK | windows.JOB_OBJECT_LIMIT_SILENT_BREAKAWAY_OK)
	if flags&breakaway != 0 {
		return fmt.Errorf("Job permits process breakaway: flags=%#x", flags)
	}
	return nil
}

// WaitEmpty waits until Windows reports that the singleton process assigned to
// the job has terminated. A process-handle wait alone does not verify the Job's
// accounting state after asynchronous termination.
func (j *Job) WaitEmpty(timeout time.Duration) error {
	if j == nil || j.handle == 0 {
		return nil
	}
	deadline := time.Now().Add(timeout)
	for {
		var info basicAccountingInformation
		if err := windows.QueryInformationJobObject(j.handle, windows.JobObjectBasicAccountingInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil); err != nil {
			return err
		}
		if info.ActiveProcesses == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for confined job to become empty")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (j *Job) Close() error {
	if j == nil || j.handle == 0 {
		return nil
	}
	handle := j.handle
	j.handle = 0
	return windows.CloseHandle(handle)
}
