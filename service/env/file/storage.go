package file

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/env"
)

// Storage provides file-based environment variable storage.
// Variables are stored in KEY=VALUE format, one per line.
// Lines starting with # are treated as comments.
type Storage struct {
	filepath   string
	autoCreate bool
	fileMode   os.FileMode
	dirMode    os.FileMode
	mutex      sync.RWMutex
}

// Verify Storage implements env.Storage
var _ env.Storage = (*Storage)(nil)

// NewStorage creates a new file-based storage.
// If autoCreate is true, the file will be created if it doesn't exist.
func NewStorage(filepath string, autoCreate bool, fileMode, dirMode os.FileMode) *Storage {
	if fileMode == 0 {
		fileMode = 0644
	}
	if dirMode == 0 {
		dirMode = 0755
	}

	return &Storage{
		filepath:   filepath,
		autoCreate: autoCreate,
		fileMode:   fileMode,
		dirMode:    dirMode,
	}
}

// Get retrieves a value by key from the file.
func (s *Storage) Get(_ context.Context, key string) (string, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	file, err := os.Open(s.filepath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", env.ErrVariableNotFound
		}
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if k, v := parseLine(scanner.Text()); k == key {
			return v, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	return "", env.ErrVariableNotFound
}

func (s *Storage) Set(_ context.Context, key, value string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.autoCreate {
		if err := s.ensureFile(); err != nil {
			return err
		}
	}

	return s.updateFile(key, value, false)
}

func (s *Storage) Delete(_ context.Context, key string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.updateFile(key, "", true)
}

func (s *Storage) List(_ context.Context) (map[string]string, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	result := make(map[string]string)
	file, err := os.Open(s.filepath)
	if err != nil {
		if os.IsNotExist(err) {
			return result, nil
		}
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if k, v := parseLine(scanner.Text()); k != "" {
			result[k] = v
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

func parseLine(text string) (key, value string) {
	line := strings.TrimSpace(text)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", ""
	}

	if idx := strings.Index(line, "#"); idx != -1 {
		line = strings.TrimSpace(line[:idx])
	}

	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", ""
	}

	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func (s *Storage) updateFile(key, value string, isDelete bool) error {
	lines, err := s.readAllLines()
	if err != nil {
		return err
	}

	updatedLines := s.processLines(lines, key, value, isDelete)
	return s.writeAllLines(updatedLines)
}

func (s *Storage) readAllLines() ([]string, error) {
	file, err := os.Open(s.filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	lines := make([]string, 0, 100)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return lines, nil
}

func (s *Storage) processLines(lines []string, key, value string, isDelete bool) []string {
	result := make([]string, 0, len(lines))
	updated := false

	for _, line := range lines {
		if k, _ := parseLine(line); k == key {
			if !isDelete {
				if idx := strings.Index(line, "#"); idx != -1 {
					result = append(result, fmt.Sprintf("%s=%s %s", key, value, line[idx:]))
				} else {
					result = append(result, fmt.Sprintf("%s=%s", key, value))
				}
			}
			updated = true
		} else {
			result = append(result, line)
		}
	}

	if !updated && !isDelete {
		result = append(result, fmt.Sprintf("%s=%s", key, value))
	}

	return result
}

func (s *Storage) writeAllLines(lines []string) error {
	tempPath := s.filepath + ".tmp"

	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, s.fileMode)
	if err != nil {
		return err
	}

	success := false
	defer func() {
		if !success {
			_ = os.Remove(tempPath)
		}
	}()

	writer := bufio.NewWriter(file)
	for _, line := range lines {
		if _, err := fmt.Fprintln(writer, line); err != nil {
			return err
		}
	}

	if err := writer.Flush(); err != nil {
		return err
	}

	if err := file.Sync(); err != nil {
		return err
	}

	if err := file.Close(); err != nil {
		return err
	}

	success = true

	maxRetries := 1
	if runtime.GOOS == "windows" {
		maxRetries = 3
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := os.Rename(tempPath, s.filepath); err != nil {
			if attempt < maxRetries && (os.IsExist(err) || strings.Contains(err.Error(), "being used by another process")) {
				time.Sleep(10 * time.Millisecond)
				continue
			}

			if os.IsExist(err) || strings.Contains(err.Error(), "being used by another process") {
				_ = os.Remove(s.filepath)
				if renameErr := os.Rename(tempPath, s.filepath); renameErr != nil {
					return env.NewRenameTempFileAfterRemoveError(renameErr)
				}
				return nil
			}
			return env.NewRenameTempFileError(attempt+1, err)
		}
		return nil
	}

	return env.NewRenameTempFileError(maxRetries+1, nil)
}

func (s *Storage) ensureFile() error {
	if _, err := os.Stat(s.filepath); err == nil {
		return nil
	}

	dir := filepath.Dir(s.filepath)
	if err := os.MkdirAll(dir, s.dirMode); err != nil {
		return err
	}

	file, err := os.OpenFile(s.filepath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, s.fileMode)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}

	if err := file.Close(); err != nil {
		return err
	}

	return nil
}
