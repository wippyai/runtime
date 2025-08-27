package env

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ponyruntime/pony/api/supervisor"
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

	return result, scanner.Err()
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

	return lines, scanner.Err()
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

	return os.Rename(tempPath, s.filepath)
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

	return file.Close()
}

func (s *FileStorage) closeFile(file *os.File) {
	if err := file.Close(); err != nil {
		s.log.Warn("failed to close file", zap.Error(err))
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
