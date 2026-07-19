package auth

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestAcceptInviteMintsCodesAndConsumes(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenUserStore(filepath.ToSlash(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	admin, err := store.Create(context.Background(), "admin@example.com", "password123", "Admin", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := store.CreateInvite(context.Background(), admin.ID, "", 7)
	if err != nil {
		t.Fatal(err)
	}

	uid := uuid.NewString()
	u, codes, gen, err := store.AcceptInvite(context.Background(), inv.RawToken, "friend@example.com", "password123", "Friend", 0, uid)
	if err != nil {
		t.Fatal(err)
	}
	if u.IsAdmin {
		t.Fatal("invitee should not be admin")
	}
	if u.ID != uid {
		t.Fatal("user id mismatch")
	}
	if len(codes) != RecoveryCodeCount || gen != 1 {
		t.Fatalf("codes=%d gen=%d", len(codes), gen)
	}
	if len(NormalizeRecoveryCode(codes[0])) != recoveryCodeChars {
		t.Fatalf("code entropy chars=%d", len(NormalizeRecoveryCode(codes[0])))
	}
	if !u.NeedsRecoveryCodesAck() {
		t.Fatal("needs ack")
	}
	if _, err := store.ValidateInviteToken(context.Background(), inv.RawToken); !errors.Is(err, ErrInviteUsed) {
		t.Fatalf("want used got %v", err)
	}

	_, _, _, err = store.AcceptInvite(context.Background(), inv.RawToken, "other@example.com", "password123", "Other", 0, uuid.NewString())
	if !errors.Is(err, ErrInviteUsed) {
		t.Fatalf("second accept: %v", err)
	}
}

func TestAcceptInviteEmailConflictAndFull(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenUserStore(filepath.ToSlash(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	admin, err := store.Create(context.Background(), "admin@example.com", "password123", "Admin", true, 2)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := store.CreateInvite(context.Background(), admin.ID, "", 7)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = store.AcceptInvite(context.Background(), inv.RawToken, "admin@example.com", "password123", "X", 2, uuid.NewString())
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("want conflict got %v", err)
	}

	// Full server + taken email must still be 409 (email checked first).
	invFull, err := store.CreateInvite(context.Background(), admin.ID, "", 7)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.AcceptInvite(context.Background(), invFull.RawToken, "b@example.com", "password123", "B", 2, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	inv3, err := store.CreateInvite(context.Background(), admin.ID, "", 7)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = store.AcceptInvite(context.Background(), inv3.RawToken, "admin@example.com", "password123", "C", 2, uuid.NewString())
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("full+taken want conflict got %v", err)
	}
	_, _, _, err = store.AcceptInvite(context.Background(), inv3.RawToken, "c@example.com", "password123", "C", 2, uuid.NewString())
	if !errors.Is(err, ErrUserLimit) {
		t.Fatalf("want limit got %v", err)
	}
}

func TestAcceptInviteNeverAdminOnEmpty(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenUserStore(filepath.ToSlash(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// Seed invite without users: create admin, invite, delete isn't available — simulate by
	// creating invite then wiping users is hard; instead accept when only invite exists:
	admin, err := store.Create(context.Background(), "admin@example.com", "password123", "Admin", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := store.CreateInvite(context.Background(), admin.ID, "", 7)
	if err != nil {
		t.Fatal(err)
	}
	// Manually delete admin row to leave invite on empty users (operator purge edge).
	if _, err := store.db.Exec(`DELETE FROM users`); err != nil {
		t.Fatal(err)
	}
	u, _, _, err := store.AcceptInvite(context.Background(), inv.RawToken, "x@example.com", "password123", "X", 0, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if u.IsAdmin {
		t.Fatal("stale invite must not grant admin on empty DB")
	}
}

func TestAcceptInviteConcurrentOnlyOneWins(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenUserStore(filepath.ToSlash(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	admin, err := store.Create(context.Background(), "admin@example.com", "password123", "Admin", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := store.CreateInvite(context.Background(), admin.ID, "", 7)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			email := "u" + string(rune('a'+i)) + "@example.com"
			_, _, _, err := store.AcceptInvite(context.Background(), inv.RawToken, email, "password123", "U", 0, uuid.NewString())
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	ok, used := 0, 0
	for err := range results {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrInviteUsed), errors.Is(err, ErrConflict):
			used++
		default:
			t.Fatalf("unexpected: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("ok=%d used=%d", ok, used)
	}
}

func TestAckAndRegenerateRecoveryCodes(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenUserStore(filepath.ToSlash(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	admin, err := store.Create(context.Background(), "admin@example.com", "password123", "Admin", true, 0)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := store.CreateInvite(context.Background(), admin.ID, "", 7)
	if err != nil {
		t.Fatal(err)
	}
	u, codes1, gen1, err := store.AcceptInvite(context.Background(), inv.RawToken, "f@example.com", "password123", "F", 0, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AckRecoveryCodes(context.Background(), u.ID, gen1); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetByID(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.NeedsRecoveryCodesAck() {
		t.Fatal("acked")
	}
	codes2, gen2, err := store.RegenerateRecoveryCodes(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gen2 != gen1+1 {
		t.Fatalf("gen2=%d", gen2)
	}
	if len(codes2) != RecoveryCodeCount {
		t.Fatal(len(codes2))
	}
	same := true
	for i := range codes1 {
		if codes1[i] != codes2[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("regenerate must return new codes")
	}
	got, err = store.GetByID(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.NeedsRecoveryCodesAck() {
		t.Fatal("regenerate must clear ack")
	}
	if err := store.AckRecoveryCodes(context.Background(), u.ID, gen1); !errors.Is(err, ErrAckGenerationMismatch) {
		t.Fatalf("stale ack: %v", err)
	}
	if _, err := store.db.Exec(`UPDATE users SET recovery_codes_generation = 1 WHERE id = ?`, admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.AckRecoveryCodes(context.Background(), admin.ID, 1); !errors.Is(err, ErrAckNoCodes) {
		t.Fatalf("ack without codes: %v", err)
	}
}

func TestCreateAutoAcksRecovery(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenUserStore(filepath.ToSlash(dir))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	u, err := store.Create(context.Background(), "a@example.com", "password123", "A", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if u.NeedsRecoveryCodesAck() {
		t.Fatal("CLI/seed create should auto-ack")
	}
}
