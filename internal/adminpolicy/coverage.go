// Package adminpolicy checks persisted administrator grants.
package adminpolicy

import "github.com/willunylabs/amsonia-core"

// Coverage records valid tenant-wide control grants per subject.
type Coverage map[amsonia.SubjectID]map[string]bool

// Add validates a persisted control grant, including grants with narrower scopes.
func (c Coverage) Add(subject amsonia.SubjectID, grant amsonia.EffectiveGrant) error {
	if err := subject.Validate(); err != nil {
		return amsonia.ErrInvalidPolicy
	}
	if err := grant.Validate(grant.Permission); err != nil {
		return err
	}
	if grant.Scope == amsonia.ScopeTenant {
		if c[subject] == nil {
			c[subject] = make(map[string]bool)
		}
		c[subject][string(grant.Permission)] = true
	}
	return nil
}

// Complete reports whether one subject holds all required permissions.
// Call it after validating every candidate grant.
func (c Coverage) Complete(permissions []string) bool {
	for _, grants := range c {
		all := true
		for _, permission := range permissions {
			if !grants[permission] {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}
