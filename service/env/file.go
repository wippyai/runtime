package env

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

	"github.com/wippyai/runtime/api/supervisor"
	"go.uber.org/zap"
)

type FileStorage struct {
	filepath   string
	autoCreate bool
	fileMode   os.FileMode
	dirMode    os.FileMode
	mutex      sync.RWMutex
	log        *zap.Logger
}

func NewFileStorage(filepath string, autoCreate bool, fileMode, dirMode os.FileMode, log *zap.Logger) *FileStorage {
	if fileMode == 0 {
		fileMode = 0644
	}
	if dirMode == 0 {
		dirMode = 0755
	}

	return &FileStorage{
		filepath:   filepath,
		autoCreate: autoCreate,
		fileMode:   fileMode,
		dirMode:    dirMode,
		log:        log,
	}
}

func (s *FileStorage) Get(_ context.Context, key string) (string, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	file, err := os.Open(s.filepath)
	if err != nil {
		return "", err
	}
	defer s.closeFile(file)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if k, v := parseLine(scanner.Text()); k == key {
			return v, nil
		}
	}

	// Check for scanner errors
	if err := scanner.Err(); err != nil {
		return "", err
	}

	return "", os.ErrNotExist
}

func (s *FileStorage) Set(_ context.Context, key, value string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.autoCreate {
		if err := s.ensureFile(); err != nil {
			return err
		}
	}

	return s.updateFile(key, value, false)
}

func (s *FileStorage) Delete(_ context.Context, key string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.updateFile(key, "", true)
}

func (s *FileStorage) List(_ context.Context) (map[string]string, error) {
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
	defer s.closeFile(file)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if k, v := parseLine(scanner.Text()); k != "" {
			result[k] = v
		}
	}

	// Check for scanner errors
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

func (s *FileStorage) updateFile(key, value string, isDelete bool) error {
	lines, err := s.readAllLines()
	if err != nil {
		return err
	}

	updatedLines := s.processLines(lines, key, value, isDelete)
	return s.writeAllLines(updatedLines)
}

func (s *FileStorage) readAllLines() ([]string, error) {
	file, err := os.Open(s.filepath)
	if err != nil {
		return nil, err
	}
	defer s.closeFile(file)

	lines := make([]string, 0, 100) // Pre-allocate with reasonable capacity
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	// Ensure scanner is done and file is fully read
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return lines, nil
}

func (s *FileStorage) processLines(lines []string, key, value string, isDelete bool) []string {
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

func (s *FileStorage) writeAllLines(lines []string) error {
	tempPath := s.filepath + ".tmp"

	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, s.fileMode)
	if err != nil {
		return err
	}

	success := false
	defer func() {
		if !success {
			if err := os.Remove(tempPath); err != nil {
				s.log.Warn("failed to remove temp file", zap.String("path", tempPath), zap.Error(err))
			}
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

	// Close the file before rename to avoid Windows file locking issues
	if err := file.Close(); err != nil {
		return err
	}

	success = true

	// On Windows, we need to ensure the file handle is fully released
	// before attempting to rename. This is especially important for tests.
	maxRetries := 1
	if runtime.GOOS == "windows" {
		maxRetries = 3
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := os.Rename(tempPath, s.filepath); err != nil {
			if attempt < maxRetries && (os.IsExist(err) || strings.Contains(err.Error(), "being used by another process")) {
				// Wait a bit before retrying on Windows
				time.Sleep(10 * time.Millisecond)
				continue
			}

			// If rename fails due to file being in use, try to remove the target first
			if os.IsExist(err) || strings.Contains(err.Error(), "being used by another process") {
				// Try to remove the target file first
				if removeErr := os.Remove(s.filepath); removeErr != nil {
					s.log.Warn("failed to remove target file before rename", zap.String("path", s.filepath), zap.Error(removeErr))
				}
				// Try rename again
				if renameErr := os.Rename(tempPath, s.filepath); renameErr != nil {
					return fmt.Errorf("failed to rename temp file after removing target: %w", renameErr)
				}
				return nil
			}
			return fmt.Errorf("failed to rename temp file: %w", err)
		}
		return nil
	}

	return fmt.Errorf("failed to rename temp file after %d attempts", maxRetries+1)
}

func (s *FileStorage) ensureFile() error {
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

	// Ensure the file is properly closed
	if err := file.Close(); err != nil {
		return err
	}

	return nil
}

func (s *FileStorage) closeFile(file *os.File) {
	if err := file.Close(); err != nil {
		s.log.Warn("failed to close file", zap.Error(err))
		// On Windows, try to force close if normal close fails
		if runtime.GOOS == "windows" {
			// Give the OS a moment to release the handle
			time.Sleep(5 * time.Millisecond)
		}
	}
}

func (s *FileStorage) Start(_ context.Context) (<-chan any, error) {
	statusCh := make(chan any, 1)
	statusCh <- supervisor.Running
	return statusCh, nil
}

func (s *FileStorage) Stop(_ context.Context) error {
	return nil
}
