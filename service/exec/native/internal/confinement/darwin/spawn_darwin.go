// SPDX-License-Identifier: MPL-2.0

//go:build darwin && cgo

package darwin

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <CoreFoundation/CoreFoundation.h>
#include <Security/Security.h>
#include <errno.h>
#include <signal.h>
#include <spawn.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/wait.h>

#ifndef POSIX_SPAWN_CLOEXEC_DEFAULT
#define POSIX_SPAWN_CLOEXEC_DEFAULT 0x4000
#endif

extern char **environ;

static int wippy_copy_cdhash(SecStaticCodeRef code, char *output, size_t output_len) {
	CFDictionaryRef info = NULL;
	OSStatus status = SecCodeCopySigningInformation(code, kSecCSSigningInformation, &info);
	if (status != errSecSuccess) return (int)status;
	CFDataRef unique = (CFDataRef)CFDictionaryGetValue(info, kSecCodeInfoUnique);
	CFNumberRef status_value = (CFNumberRef)CFDictionaryGetValue(info, kSecCodeInfoStatus);
	uint32_t status_flags = 0;
	if (status_value == NULL || CFGetTypeID(status_value) != CFNumberGetTypeID() ||
		!CFNumberGetValue(status_value, kCFNumberSInt32Type, &status_flags) ||
		(status_flags & (kSecCodeStatusHard | kSecCodeStatusKill)) !=
			(kSecCodeStatusHard | kSecCodeStatusKill)) {
		CFRelease(info);
		return -20001;
	}
	if (unique == NULL || CFGetTypeID(unique) != CFDataGetTypeID()) {
		CFRelease(info);
		return -1;
	}
	CFIndex length = CFDataGetLength(unique);
	if (length <= 0 || output_len < (size_t)(length * 2 + 1)) {
		CFRelease(info);
		return -2;
	}
	const UInt8 *bytes = CFDataGetBytePtr(unique);
	static const char hex[] = "0123456789abcdef";
	for (CFIndex i = 0; i < length; i++) {
		output[i * 2] = hex[bytes[i] >> 4];
		output[i * 2 + 1] = hex[bytes[i] & 15];
	}
	output[length * 2] = '\0';
	CFRelease(info);
	return 0;
}

static int wippy_static_cdhash(const char *path, char *output, size_t output_len) {
	CFStringRef value = CFStringCreateWithCString(NULL, path, kCFStringEncodingUTF8);
	if (value == NULL) return -3;
	CFURLRef url = CFURLCreateWithFileSystemPath(NULL, value, kCFURLPOSIXPathStyle, false);
	CFRelease(value);
	if (url == NULL) return -4;
	SecStaticCodeRef code = NULL;
	OSStatus status = SecStaticCodeCreateWithPath(url, kSecCSDefaultFlags, &code);
	CFRelease(url);
	if (status != errSecSuccess) return (int)status;
	status = (OSStatus)wippy_copy_cdhash(code, output, output_len);
	CFRelease(code);
	return (int)status;
}

static int wippy_verify_dynamic_code(pid_t pid, const char *expected_cdhash) {
	CFNumberRef pid_number = CFNumberCreate(NULL, kCFNumberIntType, &pid);
	if (pid_number == NULL) return -5;
	const void *keys[] = { kSecGuestAttributePid };
	const void *values[] = { pid_number };
	CFDictionaryRef attributes = CFDictionaryCreate(NULL, keys, values, 1,
		&kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	CFRelease(pid_number);
	if (attributes == NULL) return -6;
	SecCodeRef code = NULL;
	OSStatus status = SecCodeCopyGuestWithAttributes(NULL, attributes, kSecCSDefaultFlags, &code);
	CFRelease(attributes);
	if (status != errSecSuccess) return (int)status;

	char requirement_text[160];
	int count = snprintf(requirement_text, sizeof(requirement_text), "cdhash H\"%s\"", expected_cdhash);
	if (count <= 0 || (size_t)count >= sizeof(requirement_text)) {
		CFRelease(code);
		return -7;
	}
	CFStringRef text = CFStringCreateWithCString(NULL, requirement_text, kCFStringEncodingUTF8);
	SecRequirementRef requirement = NULL;
	status = SecRequirementCreateWithString(text, kSecCSDefaultFlags, &requirement);
	CFRelease(text);
	if (status == errSecSuccess) {
		status = SecCodeCheckValidity(code, kSecCSStrictValidate, requirement);
	}
	if (requirement != NULL) CFRelease(requirement);
	CFRelease(code);
	return (int)status;
}

static int wippy_spawn_verified(const char *path, const char *expected_cdhash,
	int stdin_fd, int stdout_fd, int stderr_fd, int policy_fd, int status_fd,
	int workdir_fd, pid_t *child) {
	posix_spawn_file_actions_t actions;
	posix_spawnattr_t attributes;
	int result = posix_spawn_file_actions_init(&actions);
	if (result != 0) return result;
	result = posix_spawnattr_init(&attributes);
	if (result != 0) {
		posix_spawn_file_actions_destroy(&actions);
		return result;
	}
	int sources[] = {stdin_fd, stdout_fd, stderr_fd, policy_fd, status_fd, workdir_fd};
	for (int target = 0; target < 6 && result == 0; target++) {
		result = posix_spawn_file_actions_adddup2(&actions, sources[target], target);
	}
	short flags = POSIX_SPAWN_START_SUSPENDED | POSIX_SPAWN_CLOEXEC_DEFAULT;
	if (result == 0) result = posix_spawnattr_setflags(&attributes, flags);
	char *const argv[] = {(char *)path, NULL};
	char *const envp[] = {NULL};
	if (result == 0) result = posix_spawn(child, path, &actions, &attributes, argv, envp);
	posix_spawnattr_destroy(&attributes);
	posix_spawn_file_actions_destroy(&actions);
	if (result != 0) return result;

	result = wippy_verify_dynamic_code(*child, expected_cdhash);
	if (result != 0) {
		kill(*child, SIGKILL);
		waitpid(*child, NULL, 0);
		return result;
	}
	if (kill(*child, SIGCONT) != 0) {
		result = errno;
		kill(*child, SIGKILL);
		waitpid(*child, NULL, 0);
		return result;
	}
	return 0;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"unsafe"
)

func StaticCDHash(path string) (string, error) {
	value := C.CString(path)
	defer C.free(unsafe.Pointer(value))
	buffer := make([]byte, 129)
	result := C.wippy_static_cdhash(value, (*C.char)(unsafe.Pointer(&buffer[0])), C.size_t(len(buffer)))
	if result != 0 {
		return "", fmt.Errorf("read helper code identity: status %d", int(result))
	}
	for index, current := range buffer {
		if current == 0 {
			return string(buffer[:index]), nil
		}
	}
	return "", errors.New("helper code identity is not terminated")
}

func SpawnVerified(path, expectedCDHash string, files [6]*os.File) (int, error) {
	if path == "" || expectedCDHash == "" {
		return 0, errors.New("missing helper path or code identity")
	}
	for _, file := range files {
		if file == nil {
			return 0, errors.New("missing helper descriptor")
		}
	}
	pathValue := C.CString(path)
	digestValue := C.CString(expectedCDHash)
	defer C.free(unsafe.Pointer(pathValue))
	defer C.free(unsafe.Pointer(digestValue))
	var child C.pid_t
	result := C.wippy_spawn_verified(pathValue, digestValue,
		C.int(files[0].Fd()), C.int(files[1].Fd()), C.int(files[2].Fd()),
		C.int(files[3].Fd()), C.int(files[4].Fd()), C.int(files[5].Fd()), &child)
	if result != 0 {
		return 0, fmt.Errorf("spawn verified helper: status %d", int(result))
	}
	return int(child), nil
}
