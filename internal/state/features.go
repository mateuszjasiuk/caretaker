package state

import (
	"fmt"
	"slices"
)

// Feature is one piece of work split over several worktrees: a manager session
// plans it and starts the other worktrees. Keys are "repo/worktree", and the
// feature's own key is "repo/name", so a feature lives in one repo.
type Feature struct {
	Manager string `json:"manager"`
	Notes   string `json:"notes,omitempty"`
	// Worktrees are the members other than the manager.
	Worktrees []string `json:"worktrees,omitempty"`
}

// NewFeature records a feature led by manager. It refuses a name already in use
// and a manager that already belongs to another feature.
func (s *State) NewFeature(key, manager, notes string) error {
	if _, ok := s.Features[key]; ok {
		return fmt.Errorf("feature %s already exists", key)
	}
	if other, ok := s.FeatureOf(manager); ok {
		return fmt.Errorf("%s already belongs to feature %s", manager, other)
	}

	if s.Features == nil {
		s.Features = map[string]*Feature{}
	}
	s.Features[key] = &Feature{Manager: manager, Notes: notes}
	return nil
}

// AddToFeature makes worktree a member of the feature. Adding a member again
// changes nothing; a worktree in another feature is refused.
func (s *State) AddToFeature(key, worktree string) error {
	f, ok := s.Features[key]
	if !ok {
		return fmt.Errorf("no feature %s", key)
	}
	if other, ok := s.FeatureOf(worktree); ok {
		if other == key {
			return nil
		}
		return fmt.Errorf("%s already belongs to feature %s", worktree, other)
	}

	f.Worktrees = append(f.Worktrees, worktree)
	return nil
}

// DropFeature forgets a feature. Its worktrees stay as they are.
func (s *State) DropFeature(key string) error {
	if _, ok := s.Features[key]; !ok {
		return fmt.Errorf("no feature %s", key)
	}
	delete(s.Features, key)
	return nil
}

// FeatureOf returns the key of the feature worktree belongs to, as its manager
// or as a member.
func (s *State) FeatureOf(worktree string) (string, bool) {
	for key, f := range s.Features {
		if f.Manager == worktree || slices.Contains(f.Worktrees, worktree) {
			return key, true
		}
	}
	return "", false
}
