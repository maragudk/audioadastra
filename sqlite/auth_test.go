package sqlite_test

import (
	"sync"
	"testing"

	"maragu.dev/is"

	"app/model"
	"app/sqlitetest"
)

func TestDatabase_GetUser(t *testing.T) {
	t.Run("should get the admin user", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t, sqlitetest.WithFixtures("admin"))

		user, err := db.GetUser(t.Context(), "u_f4958e9cd27a553b08092c790ea44fbb")
		is.NotError(t, err)
		is.Equal(t, model.UserID("u_f4958e9cd27a553b08092c790ea44fbb"), user.ID)
		is.Equal(t, model.DID("did:plc:adminadminadminadminadmi"), user.DID)
		is.True(t, user.Active)
		is.True(t, !user.Created.T.IsZero())
	})

	t.Run("should not get nonexistent user", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t, sqlitetest.WithFixtures("admin"))

		_, err := db.GetUser(t.Context(), "u_nonexistent")
		is.Error(t, model.ErrorUserNotFound, err)
	})
}

func TestDatabase_GetOrCreateUser(t *testing.T) {
	t.Run("should create a new active user for an unknown DID", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		user, created, err := db.GetOrCreateUser(t.Context(), "did:plc:alice")
		is.NotError(t, err)
		is.True(t, created)
		is.True(t, user.ID != "")
		is.Equal(t, model.DID("did:plc:alice"), user.DID)
		is.True(t, user.Active)
	})

	t.Run("should give concurrent first logins for one DID the same user, created once", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		var wg sync.WaitGroup
		ids := make([]model.UserID, 8)
		createds := make([]bool, 8)
		for i := range ids {
			wg.Add(1)
			go func() {
				defer wg.Done()
				user, created, err := db.GetOrCreateUser(t.Context(), "did:plc:alice")
				if err != nil {
					t.Error(err)
					return
				}
				ids[i], createds[i] = user.ID, created
			}()
		}
		wg.Wait()

		var createdCount int
		for i := range ids {
			is.Equal(t, ids[0], ids[i])
			if createds[i] {
				createdCount++
			}
		}
		is.Equal(t, 1, createdCount)
	})

	t.Run("should get the existing user for a known DID, keeping its ID and inactive flag", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		first, _, err := db.GetOrCreateUser(t.Context(), "did:plc:alice")
		is.NotError(t, err)
		is.NotError(t, db.H.Exec(t.Context(), `update users set active = 0 where id = ?`, first.ID))

		second, created, err := db.GetOrCreateUser(t.Context(), "did:plc:alice")
		is.NotError(t, err)
		is.True(t, !created)
		is.Equal(t, first.ID, second.ID)
		is.True(t, !second.Active)
	})
}

func TestDatabase_IsUserActive(t *testing.T) {
	t.Run("should report the admin user active", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t, sqlitetest.WithFixtures("admin"))

		active, err := db.IsUserActive(t.Context(), "u_f4958e9cd27a553b08092c790ea44fbb")
		is.NotError(t, err)
		is.True(t, active)
	})

	t.Run("should return not found for a nonexistent user", func(t *testing.T) {
		db := sqlitetest.NewDatabase(t)

		_, err := db.IsUserActive(t.Context(), "u_nonexistent")
		is.Error(t, model.ErrorUserNotFound, err)
	})
}
