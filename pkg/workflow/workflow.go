package workflow

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/rhysd/actionlint"
	"github.com/thombashi/gh-actionarmor/internal/pkg/common"
)

type WorkflowInfo struct {
	// FilePath is an absolute path to the GitHub Actions workflow file.
	FilePath string

	// Project is a GitHub project that contains the workflow file.
	Project *actionlint.Project

	// Config is an ActionArmor configuration file.
	Config *ActionArmorConfigFile
}

func extractWorkflows(dirPath string, logger *slog.Logger) ([]*WorkflowInfo, error) {
	var workflows []*WorkflowInfo

	proj, err := actionlint.NewProjects().At(dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to find project: %w", err)
	}
	if proj == nil {
		return nil, fmt.Errorf("failed to find project at %s", dirPath)
	}

	config, err := GetConfigFile(proj)
	if err != nil {
		if errors.Is(err, ErrConfigFileNotFound) {
			logger.Debug(err.Error())
		} else {
			return nil, fmt.Errorf("failed to get a config file path: %w", err)
		}
	}

	logger.Debug("extracting workflow files", slog.String("path", proj.WorkflowsDir()))
	entries, err := os.ReadDir(proj.WorkflowsDir())
	if err != nil {
		return nil, fmt.Errorf("failed to read workflows directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		fname := entry.Name()
		ext := filepath.Ext(fname)
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		workflowFilePath := filepath.Join(proj.WorkflowsDir(), fname)
		logger.Debug("workflow file found", slog.String("path", workflowFilePath))

		workflows = append(workflows, &WorkflowInfo{
			FilePath: workflowFilePath,
			Project:  proj,
			Config:   config,
		})
	}

	return workflows, nil
}

func toWorkflowInfo(path string) (*WorkflowInfo, error) {
	proj, err := actionlint.NewProjects().At(path)
	if err != nil {
		return nil, fmt.Errorf("failed to find project: %w", err)
	}
	if proj == nil {
		return nil, fmt.Errorf("failed to find project at %s", path)
	}

	workflow := &WorkflowInfo{
		FilePath: path,
		Project:  proj,
	}

	config, err := GetConfigFile(proj)
	if err != nil && errors.Is(err, ErrConfigFileNotFound) {
		return nil, fmt.Errorf("failed to get a config file path: %w", err)
	}

	workflow.Config = config

	return workflow, nil
}

// ListWorkflows returns a list of WorkflowInfo from the given paths.
//
// The first argument 'paths' can be a list of file paths or directories.
// If a path is a file, it is treated as a workflow file.
// If a path is a directory, it searches workflow files in the repository.
func ListWorkflows(paths []string, logger *slog.Logger) ([]*WorkflowInfo, error) {
	wfInfoMap := map[string]*WorkflowInfo{}

	for _, path := range paths {
		logger.Debug("listing workflows", slog.String("path", path))

		isFile, err := common.IsFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to check if the argument is a file: %w", err)
		}
		if isFile {
			workflow, err := toWorkflowInfo(path)
			if err != nil {
				return nil, err
			}

			wfInfoMap[path] = workflow
			continue
		}

		workflows, err := extractWorkflows(path, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to extract workflow file paths: %w", err)
		}

		for _, workflow := range workflows {
			wfInfoMap[workflow.FilePath] = workflow
		}
	}

	workflows := make([]*WorkflowInfo, 0, len(wfInfoMap))
	for _, workflow := range wfInfoMap {
		workflows = append(workflows, workflow)
	}

	return workflows, nil
}
