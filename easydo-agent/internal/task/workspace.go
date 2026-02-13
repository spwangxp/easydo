package task

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

// WorkspaceManager manages task workspaces
type WorkspaceManager struct {
	basePath string
	log      *logrus.Logger
}

// NewWorkspaceManager creates a new workspace manager
func NewWorkspaceManager(basePath string, log *logrus.Logger) *WorkspaceManager {
	wm := &WorkspaceManager{
		basePath: basePath,
		log:      log,
	}

	// Ensure base path exists with detailed logging
	if err := wm.validateAndCreateBasePath(); err != nil {
		log.Warnf("Workspace: failed to create base path: %v", err)
	} else {
		log.Infof("Workspace: base path initialized at %s", basePath)
	}

	return wm
}

// validateAndCreateBasePath validates and creates the base workspace directory
func (wm *WorkspaceManager) validateAndCreateBasePath() error {
	// Check if basePath is empty or contains invalid characters
	if wm.basePath == "" {
		return fmt.Errorf("workspace base path is empty")
	}

	// Check if path is absolute
	if !filepath.IsAbs(wm.basePath) {
		return fmt.Errorf("workspace base path must be absolute path: %s", wm.basePath)
	}

	// Check parent directory permissions
	parentDir := filepath.Dir(wm.basePath)
	if info, err := os.Stat(parentDir); err == nil && info.IsDir() {
		// Check write permission on parent directory
		testFile := filepath.Join(parentDir, ".write_test")
		if err := os.WriteFile(testFile, []byte{}, 0644); err == nil {
			os.Remove(testFile)
		} else {
			return fmt.Errorf("no write permission on parent directory %s: %w", parentDir, err)
		}
	}

	// Create base path with proper permissions
	return os.MkdirAll(wm.basePath, 0755)
}

// GetPipelineWorkspace returns the workspace directory for a pipeline
func (wm *WorkspaceManager) GetPipelineWorkspace(pipelineRunID uint64) string {
	return filepath.Join(wm.basePath, fmt.Sprintf("workspace_%d", pipelineRunID))
}

// CreateWorkspace creates the workspace directory for a pipeline run
func (wm *WorkspaceManager) CreateWorkspace(pipelineRunID uint64) (string, error) {
	workspacePath := wm.GetPipelineWorkspace(pipelineRunID)

	// Check if basePath is valid first
	if err := wm.validateBasePath(); err != nil {
		return "", fmt.Errorf("workspace base path invalid: %w", err)
	}

	// Create the workspace directory with detailed logging
	if wm.log != nil {
		wm.log.Debugf("Workspace: creating directory %s", workspacePath)
	}

	if err := os.MkdirAll(workspacePath, 0755); err != nil {
		// Provide detailed error information
		dirExists := false
		if _, err := os.Stat(workspacePath); err == nil {
			dirExists = true
		}

		errMsg := fmt.Sprintf("failed to create workspace directory: %v", err)
		if dirExists {
			errMsg = fmt.Sprintf("workspace directory exists but cannot access: %s", workspacePath)
		}

		if wm.log != nil {
			wm.log.Warnf("Workspace: %s", errMsg)
			// Log additional diagnostic info
			wm.log.Warnf("Workspace: basePath=%s, pipelineRunID=%d, finalPath=%s",
				wm.basePath, pipelineRunID, workspacePath)

			// Check write permission
			testFile := filepath.Join(workspacePath, ".test_write")
			if err := os.WriteFile(testFile, []byte{}, 0644); err != nil {
				wm.log.Warnf("Workspace: no write permission in workspace directory: %v", err)
			} else {
				os.Remove(testFile)
			}
		}

		return "", fmt.Errorf(errMsg)
	}

	if wm.log != nil {
		wm.log.Infof("Workspace: successfully created %s", workspacePath)
	}

	return workspacePath, nil
}

// validateBasePath checks if the base path is valid and accessible
func (wm *WorkspaceManager) validateBasePath() error {
	if wm.basePath == "" {
		return fmt.Errorf("base path is empty")
	}

	// Check if base path directory exists
	if _, err := os.Stat(wm.basePath); err == nil {
		// Check if we can write to the base path
		testFile := filepath.Join(wm.basePath, ".perm_test")
		if err := os.WriteFile(testFile, []byte{}, 0644); err != nil {
			return fmt.Errorf("no write permission on base path %s: %w", wm.basePath, err)
		}
		os.Remove(testFile)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("cannot access base path %s: %w", wm.basePath, err)
	}

	return nil
}

// GetTaskFilePath returns the path to the task file
func (wm *WorkspaceManager) GetTaskFilePath(workspacePath string, taskID uint64) string {
	return filepath.Join(workspacePath, fmt.Sprintf("task_%d.sh", taskID))
}

// WriteTaskFile writes the task script to a file
func (wm *WorkspaceManager) WriteTaskFile(workspacePath string, taskID uint64, script string) (string, error) {
	taskFilePath := wm.GetTaskFilePath(workspacePath, taskID)

	// Ensure the workspace directory exists
	if err := wm.ensureDirectoryExists(workspacePath); err != nil {
		return "", fmt.Errorf("workspace directory does not exist: %w", err)
	}

	// Write the script to file
	if err := os.WriteFile(taskFilePath, []byte(script), 0644); err != nil {
		return "", fmt.Errorf("failed to write task file: %w", err)
	}

	return taskFilePath, nil
}

// ensureDirectoryExists checks if directory exists and creates if needed
func (wm *WorkspaceManager) ensureDirectoryExists(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return os.MkdirAll(path, 0755)
	}
	return nil
}

// CleanupWorkspace removes the workspace directory
func (wm *WorkspaceManager) CleanupWorkspace(pipelineRunID uint64) error {
	workspacePath := wm.GetPipelineWorkspace(pipelineRunID)

	if err := os.RemoveAll(workspacePath); err != nil {
		return fmt.Errorf("failed to cleanup workspace: %w", err)
	}

	if wm.log != nil {
		wm.log.Infof("Workspace: cleaned up %s", workspacePath)
	}

	return nil
}

// GetBasePath returns the current base path
func (wm *WorkspaceManager) GetBasePath() string {
	return wm.basePath
}

// IsPathAccessible checks if a path is accessible for writing
func IsPathAccessible(path string) bool {
	// Clean the path
	cleanPath := filepath.Clean(path)

	// Check if parent directory exists and is writable
	parent := filepath.Dir(cleanPath)
	if info, err := os.Stat(parent); err == nil && info.IsDir() {
		testFile := filepath.Join(parent, ".access_test")
		if err := os.WriteFile(testFile, []byte{}, 0644); err == nil {
			os.Remove(testFile)
			return true
		}
	}

	// Check if path itself exists and is writable
	if info, err := os.Stat(cleanPath); err == nil {
		if info.IsDir() {
			testFile := filepath.Join(cleanPath, ".access_test")
			if err := os.WriteFile(testFile, []byte{}, 0644); err == nil {
				os.Remove(testFile)
				return true
			}
		}
	}

	// Check if we can create the directory
	if !strings.HasSuffix(path, string(filepath.Separator)) {
		testDir := filepath.Join(path, ".access_test_dir")
		if err := os.MkdirAll(testDir, 0755); err == nil {
			os.RemoveAll(testDir)
			return true
		}
	}

	return false
}
