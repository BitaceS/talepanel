package services

import (
	"errors"
	"testing"

	"github.com/BitaceS/talepanel/api/internal/models"
	"github.com/google/uuid"
)

func TestCanDeleteServer(t *testing.T) {
	owner, other := uuid.New(), uuid.New()
	srv := &models.Server{ID: uuid.New(), OwnerID: owner}
	cases := []struct {
		user uuid.UUID
		role string
		ok   bool
	}{
		{owner, models.RoleUser, true},
		{other, models.RoleUser, false},
		{other, models.RoleModerator, false},
		{other, models.RoleAdmin, true},
		{other, models.RoleOwner, true},
	}
	for _, c := range cases {
		err := CanDeleteServer(srv, c.user, c.role)
		if c.ok && err != nil {
			t.Errorf("%s: unexpected %v", c.role, err)
		}
		if !c.ok && !errors.Is(err, ErrServerForbidden) {
			t.Errorf("%s non-owner: err = %v, want forbidden", c.role, err)
		}
	}
}
