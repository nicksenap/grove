package workspace

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/models"
)

// WorkspaceSummary holds summary info for list --status.
type WorkspaceSummary struct {
	Name   string `json:"name"`
	Branch string `json:"branch"`
	Repos  int    `json:"repos"`
	Status string `json:"status"`
	Path   string `json:"path"`
}

// AllWorkspacesSummary returns a status summary for all workspaces.
func (s *Service) AllWorkspacesSummary() ([]WorkspaceSummary, error) {
	workspaces, err := s.State.Load()
	if err != nil {
		return nil, err
	}

	if len(workspaces) == 0 {
		return []WorkspaceSummary{}, nil
	}

	results := make([]WorkspaceSummary, len(workspaces))
	var wg sync.WaitGroup
	for i, ws := range workspaces {
		wg.Add(1)
		go func(idx int, w models.Workspace) {
			defer wg.Done()
			summary := WorkspaceSummary{
				Name:   w.Name,
				Branch: w.Branch,
				Repos:  len(w.Repos),
				Path:   w.Path,
			}

			clean, dirty, errCount := 0, 0, 0
			for _, r := range w.Repos {
				status, err := gitops.RepoStatus(r.WorktreePath)
				if err != nil {
					errCount++
				} else if status == "" {
					clean++
				} else {
					dirty++
				}
			}

			parts := []string{}
			if clean > 0 {
				parts = append(parts, fmt.Sprintf("%d clean", clean))
			}
			if dirty > 0 {
				parts = append(parts, fmt.Sprintf("%d modified", dirty))
			}
			if errCount > 0 {
				parts = append(parts, fmt.Sprintf("%d error", errCount))
			}
			summary.Status = strings.Join(parts, ", ")
			if summary.Status == "" {
				summary.Status = "empty"
			}

			results[idx] = summary
		}(i, ws)
	}
	wg.Wait()

	return results, nil
}

// Doctor checks workspace health and returns issues.
func (s *Service) Doctor(fix bool) ([]models.DoctorIssue, int, error) {
	if !fix {
		return s.doctor(false)
	}

	var issues []models.DoctorIssue
	var fixed int
	err := s.State.WithLock(func() error {
		var err error
		issues, fixed, err = s.doctor(true)
		return err
	})
	return issues, fixed, err
}

func (s *Service) doctor(fix bool) ([]models.DoctorIssue, int, error) {
	workspaces, err := s.State.Load()
	if err != nil {
		return nil, 0, err
	}

	var issues []models.DoctorIssue
	fixed := 0

	for _, ws := range workspaces {
		f, iss := s.checkWorkspaceExists(ws, fix)
		if f > 0 {
			fixed += f
		}
		issues = append(issues, iss...)
		if len(iss) > 0 {
			continue
		}

		f, iss = s.checkWorkspaceRepos(&ws, fix)
		fixed += f
		issues = append(issues, iss...)
	}

	return issues, fixed, nil
}

func (s *Service) checkWorkspaceExists(ws models.Workspace, fix bool) (int, []models.DoctorIssue) {
	if _, err := os.Stat(ws.Path); err == nil {
		return 0, nil
	}
	issue := models.DoctorIssue{
		Workspace:       ws.Name,
		Repo:            nil,
		Issue:           "workspace directory missing",
		SuggestedAction: "remove stale state entry",
	}
	if fix {
		s.State.RemoveWorkspace(ws.Name)
		return 1, []models.DoctorIssue{issue}
	}
	return 0, []models.DoctorIssue{issue}
}

func (s *Service) checkWorkspaceRepos(ws *models.Workspace, fix bool) (int, []models.DoctorIssue) {
	var issues []models.DoctorIssue
	var toRemove []string
	fixed := 0

	for _, r := range ws.Repos {
		if iss, shouldRemove := s.checkRepo(ws.Name, r); iss != nil {
			issues = append(issues, *iss)
			if fix && shouldRemove {
				toRemove = append(toRemove, r.RepoName)
				fixed++
			}
		}
	}

	if fix && len(toRemove) > 0 {
		if currentWS, err := s.State.GetWorkspace(ws.Name); err == nil && currentWS != nil {
			for _, name := range toRemove {
				currentWS.RemoveRepo(name)
			}
			s.State.UpdateWorkspace(*currentWS)
		}
	}

	return fixed, issues
}

func (s *Service) checkRepo(wsName string, r models.RepoWorktree) (*models.DoctorIssue, bool) {
	repoName := r.RepoName

	if _, err := os.Stat(r.SourceRepo); os.IsNotExist(err) {
		return &models.DoctorIssue{
			Workspace:       wsName,
			Repo:            &repoName,
			Issue:           "source repo missing",
			SuggestedAction: "remove stale repo entry",
		}, true
	}

	if _, err := os.Stat(r.WorktreePath); os.IsNotExist(err) {
		return &models.DoctorIssue{
			Workspace:       wsName,
			Repo:            &repoName,
			Issue:           "worktree directory missing",
			SuggestedAction: "remove stale repo entry",
		}, true
	}

	return nil, false
}
