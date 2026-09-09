package workspace

import (
	"fmt"
	"os"
	"path/filepath"
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
		issues, _, err := s.doctor(false)
		if err != nil {
			return issues, 0, err
		}
		trashIssues, _ := s.sweepLeftoverTrash(false)
		return append(issues, trashIssues...), 0, nil
	}

	var issues []models.DoctorIssue
	var fixed int
	err := s.State.WithLock(func() error {
		var err error
		issues, fixed, err = s.doctor(true)
		return err
	})
	if err != nil {
		return issues, fixed, err
	}

	// Unlink leftover trash outside the mutation lock so doctor --fix does not
	// hold state.lock while walking large trees.
	trashIssues, trashFixed := s.sweepLeftoverTrash(true)
	return append(issues, trashIssues...), fixed + trashFixed, nil
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

func (s *Service) sweepLeftoverTrash(fix bool) ([]models.DoctorIssue, int) {
	workspaces, err := s.State.Load()
	if err != nil {
		workspaces = nil
	}
	ownedPrefixes := ownedTrashPrefixes(workspaces)
	var issues []models.DoctorIssue
	fixed := 0
	for _, trashRoot := range leftoverTrashRoots(s, workspaces) {
		entries, err := os.ReadDir(trashRoot)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			item := filepath.Join(trashRoot, entry.Name())
			if trashOwnedByWorkspace(entry.Name(), ownedPrefixes) {
				issue := models.DoctorIssue{
					Workspace:       entry.Name(),
					Issue:           "leftover trash still belongs to a workspace",
					SuggestedAction: "restore quarantined workspace or retry delete",
				}
				issues = append(issues, issue)
				continue
			}
			issue := models.DoctorIssue{
				Workspace:       entry.Name(),
				Issue:           "leftover trash",
				SuggestedAction: "remove quarantined workspace bytes",
			}
			if fix {
				if err := UnlinkTrashPath(item); err != nil && !os.IsNotExist(err) {
					issue.SuggestedAction = "remove quarantined workspace bytes (failed: " + err.Error() + ")"
					issues = append(issues, issue)
					continue
				}
				fixed++
			}
			issues = append(issues, issue)
		}
	}
	return issues, fixed
}

func ownedTrashPrefixes(workspaces []models.Workspace) []string {
	prefixes := make([]string, 0, len(workspaces))
	for _, ws := range workspaces {
		prefixes = append(prefixes, filepath.Base(ws.Path)+"-")
	}
	return prefixes
}

func trashOwnedByWorkspace(name string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func leftoverTrashRoots(s *Service, workspaces []models.Workspace) []string {
	seen := map[string]struct{}{}
	var roots []string
	add := func(dir string) {
		if dir == "" {
			return
		}
		if _, ok := seen[dir]; ok {
			return
		}
		seen[dir] = struct{}{}
		roots = append(roots, filepath.Join(dir, trashDirName))
	}
	for _, ws := range workspaces {
		add(filepath.Dir(ws.Path))
	}
	add(s.WorkspaceDir)
	add(filepath.Join(filepath.Dir(s.State.Path), "workspaces"))
	return roots
}

func (s *Service) checkWorkspaceExists(ws models.Workspace, fix bool) (int, []models.DoctorIssue) {
	if _, err := os.Stat(ws.Path); err == nil {
		return 0, nil
	}
	if trashPath := matchingTrashItem(s, ws); trashPath != "" {
		issue := models.DoctorIssue{
			Workspace:       ws.Name,
			Repo:            nil,
			Issue:           "workspace directory missing; bytes remain in leftover trash",
			SuggestedAction: "restore quarantined workspace or retry delete",
		}
		return 0, []models.DoctorIssue{issue}
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

func matchingTrashItem(s *Service, ws models.Workspace) string {
	prefix := filepath.Base(ws.Path) + "-"
	for _, trashRoot := range leftoverTrashRoots(s, []models.Workspace{ws}) {
		entries, err := os.ReadDir(trashRoot)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), prefix) {
				return filepath.Join(trashRoot, entry.Name())
			}
		}
	}
	return ""
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
