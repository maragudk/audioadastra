package sqlite

import (
	"context"
	"errors"

	"maragu.dev/glue/sql"

	"app/model"
)

func (d *Database) GetUser(ctx context.Context, id model.UserID) (model.User, error) {
	var u model.User
	if err := d.H.Get(ctx, &u, `select * from users where id = ?`, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return u, model.ErrorUserNotFound
		}
		return u, err
	}

	return u, nil
}

// GetOrCreateUser by DID, reporting whether the user was created by this call. A new user is active.
// Two concurrent calls for a new DID both get the one user that the first of them created.
func (d *Database) GetOrCreateUser(ctx context.Context, did model.DID) (model.User, bool, error) {
	var u model.User
	var created bool
	err := d.H.InTx(ctx, func(ctx context.Context, tx *Tx) error {
		if err := tx.Exec(ctx, `insert into users (did) values (?) on conflict (did) do nothing`, did); err != nil {
			return err
		}

		var changes int
		if err := tx.Get(ctx, &changes, `select changes()`); err != nil {
			return err
		}
		created = changes == 1

		return tx.Get(ctx, &u, `select * from users where did = ?`, did)
	})
	if err != nil {
		return model.User{}, false, err
	}

	return u, created, nil
}

func (d *Database) IsUserActive(ctx context.Context, id model.UserID) (bool, error) {
	var active bool
	query := `select active from users where id = ?`
	if err := d.H.Get(ctx, &active, query, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, model.ErrorUserNotFound
		}
		return false, err
	}
	return active, nil
}

func (d *Database) GetPermissions(ctx context.Context, id model.UserID) ([]model.Permission, error) {
	var permissions []model.Permission
	query := `
		select distinct rp.permission
		from users_roles ur
			join roles_permissions rp on ur.role = rp.role
		where ur.user_id = ?
		`

	if err := d.H.Select(ctx, &permissions, query, id); err != nil {
		return nil, err
	}

	return permissions, nil
}
