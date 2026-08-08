package syncstore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreRejectsTraversalUserID(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, 1024*1024, 1024*1024)
	if err := s.Bootstrap("../etc"); err == nil {
		t.Fatal("expected invalid user id")
	}
	if _, err := s.GetManifest("../../../tmp"); err == nil {
		t.Fatal("expected invalid user id")
	}
}

func TestStoreManifestAndTravel(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, 1024*1024, 1024*1024)
	uid := "user-1"
	if err := s.Bootstrap(uid); err != nil {
		t.Fatal(err)
	}
	b, err := s.GetManifest(uid)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["schemaVersion"].(float64) != 1 {
		t.Fatalf("manifest=%v", m)
	}
	travels, ok := m["travels"].([]any)
	if !ok || len(travels) != 0 {
		t.Fatalf("travels=%v", m["travels"])
	}

	body := []byte(`{"schemaVersion":1,"updatedAt":"2026-07-19T12:00:00Z","travels":[]}`)
	if err := s.PutManifest(uid, body); err != nil {
		t.Fatal(err)
	}
	if err := s.PutTravel(uid, "t1", []byte(`{"id":"t1","name":"Trip","items":[]}`)); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTravel(uid, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("empty travel")
	}
	if err := s.DeleteTravel(uid, "t1"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteTravel(uid, "t1"); err != nil {
		t.Fatal("idempotent delete")
	}
	if _, err := s.GetTravel(uid, "t1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestDeleteTravelCascadesAttachmentFiles(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, 1024*1024, 1024*1024)
	uid := "user-cascade"
	if err := s.Bootstrap(uid); err != nil {
		t.Fatal(err)
	}
	if err := s.PutTravel(uid, "t1", []byte(`{"id":"t1","name":"Trip","items":[]}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutFile(uid, "t1", "i1", "photo.jpg", []byte("abc")); err != nil {
		t.Fatal(err)
	}
	filesDir := filepath.Join(dir, "users", uid, "files", "t1")
	if _, err := os.Stat(filesDir); err != nil {
		t.Fatalf("expected attachment dir: %v", err)
	}
	if err := s.DeleteTravel(uid, "t1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filesDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected attachment tree removed, got %v", err)
	}
	if _, err := s.GetTravel(uid, "t1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want travel not found, got %v", err)
	}
}

func TestStoreFilesDualPrefix(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, 1024*1024, 1024*1024)
	uid := "u2"
	_ = s.Bootstrap(uid)
	fileID, err := s.PutFile(uid, "t1", "i1", "photo.jpg", []byte("abc"))
	if err != nil {
		t.Fatal(err)
	}
	if fileID != "files/t1/i1/photo.jpg" {
		t.Fatalf("fileID=%s", fileID)
	}
	rel, err := NormalizeFileID("files/t1/i1/photo.jpg")
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := s.GetFile(uid, rel)
	if err != nil || string(b) != "abc" {
		t.Fatalf("get=%s err=%v", b, err)
	}
	rel2, _ := NormalizeFileID("t1/i1/photo.jpg")
	b2, _, err := s.GetFile(uid, rel2)
	if err != nil || string(b2) != "abc" {
		t.Fatal(err)
	}
	if err := s.DeleteFile(uid, rel); err != nil {
		t.Fatal(err)
	}
}

func TestQuotaExceeded(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, 10, 1024)
	uid := "u3"
	_ = s.Bootstrap(uid)
	if _, err := s.PutFile(uid, "t1", "i1", "big.bin", make([]byte, 20)); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("want quota, got %v", err)
	}
}

func TestPayloadTooLarge(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, 1024*1024, 5)
	uid := "u4"
	_ = s.Bootstrap(uid)
	if _, err := s.PutFile(uid, "t1", "i1", "x.bin", []byte("123456")); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("want payload, got %v", err)
	}
}

func TestWalkUsageSkipsSymlink(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "a.txt"), []byte("hi"), 0o644)
	n, err := WalkUsage(root)
	if err != nil || n != 2 {
		t.Fatalf("usage=%d err=%v", n, err)
	}
}
