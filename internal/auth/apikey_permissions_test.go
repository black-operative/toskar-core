package auth_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/internal/store"
)

func TestAPIKeyPermissions(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	m := auth.NewAPIKeyManager(db.SQL, auth.NewSecretStore(dir))
	ctx := context.Background()

	rec, secret, err := m.Create(ctx, "bot")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Permissions != auth.DefaultAPIKeyPermissions() {
		t.Fatalf("new key = %+v", rec.Permissions)
	}
	if _, err := m.SetPermissions(ctx, rec.ID, auth.APIKeyPermissions{Memory: "sometimes", Knowledge: auth.UseNever, Tools: auth.ToolsNone}); err == nil {
		t.Fatal("bad level accepted")
	}
	narrow := auth.APIKeyPermissions{Memory: auth.UseNever, Knowledge: auth.UseOnRequest, Tools: auth.ToolsReadOnly}
	if got, err := m.SetPermissions(ctx, rec.ID, narrow); err != nil || got.Permissions != narrow {
		t.Fatalf("set = %+v, %v", got, err)
	}
	if got, err := m.Verify(ctx, secret); err != nil || got.Permissions != narrow {
		t.Fatalf("verify = %+v, %v", got, err)
	}
	// A rotated key keeps what the old one was allowed.
	rotated, _, err := m.Rotate(ctx, rec.ID)
	if err != nil || rotated.Permissions != narrow {
		t.Fatalf("rotate = %+v, %v", rotated, err)
	}
	list, _ := m.List(ctx)
	if len(list) != 1 || list[0].Permissions != narrow {
		t.Fatalf("list = %+v", list)
	}
}
