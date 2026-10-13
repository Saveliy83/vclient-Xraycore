package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"example.com/vclient/internal/config"
)

func TestProfileStoreAddUpdateDeleteAndDuplicates(t *testing.T) {
	store, err := NewProfileStore(filepath.Join(t.TempDir(), "profiles.json"))
	if err != nil {
		t.Fatalf("NewProfileStore() error = %v", err)
	}
	first := testProfile("main")
	if err := store.Add(first); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if err := store.Add(first); err != nil {
		t.Fatalf("Add() duplicate error = %v", err)
	}
	profiles := store.List()
	if len(profiles) != 1 {
		t.Fatalf("List() = %#v, want identical profile to be ignored", profiles)
	}
	renamedDuplicate := first
	renamedDuplicate.Name = "renamed"
	if err := store.Add(renamedDuplicate); err != nil {
		t.Fatalf("Add() renamed duplicate error = %v", err)
	}
	distinct := first
	distinct.Address = "other.example.com"
	if err := store.Add(distinct); err != nil {
		t.Fatalf("Add() distinct profile error = %v", err)
	}
	profiles = store.List()
	if len(profiles) != 2 || profiles[1].Name != "main (2)" {
		t.Fatalf("List() = %#v, want distinct profile to receive unique name", profiles)
	}
	updated := profiles[0]
	updated.Name = "updated"
	if err := store.Update(0, updated); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	conflictingName := profiles[1]
	conflictingName.Name = profiles[1].Name
	if err := store.Update(0, conflictingName); err == nil {
		t.Fatal("Update() duplicate name error = nil")
	}
	if err := store.Delete(1); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if got := store.List(); len(got) != 1 || got[0].Name != "updated" {
		t.Fatalf("List() after update/delete = %#v", got)
	}
	if _, ok := store.Get(-1); ok {
		t.Fatal("Get(-1) returned a profile")
	}
	if err := store.Update(4, first); err == nil {
		t.Fatal("Update() out-of-range error = nil")
	}
	if err := store.Delete(4); err == nil {
		t.Fatal("Delete() out-of-range error = nil")
	}
}

func TestProfileStoreAtomicWritesLeaveNoTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profiles.json")
	store, err := NewProfileStore(path)
	if err != nil {
		t.Fatalf("NewProfileStore() error = %v", err)
	}
	if err := store.Add(testProfile("atomic")); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "profiles.json" {
		t.Fatalf("files after atomic write = %v, want profiles.json only", entries)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("profile store permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestCorruptProfileStoreIsBackedUp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatalf("write corrupt store: %v", err)
	}
	store, err := NewProfileStore(path)
	if err == nil || !strings.Contains(err.Error(), "decode profile store") {
		t.Fatalf("NewProfileStore() error = %v, want decode error", err)
	}
	if got := store.List(); len(got) != 0 {
		t.Fatalf("corrupt store profiles = %v, want empty", got)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("backup file was not created: %v", err)
	}
	if _, err := NewProfileStore(path + ".missing"); err != nil {
		t.Fatalf("NewProfileStore() missing path error = %v", err)
	}
}

func TestCorruptProfileStoreIsNotOverwrittenWhenBackupExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	original := []byte("{broken")
	backup := []byte("previous backup")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("write corrupt store: %v", err)
	}
	if err := os.WriteFile(path+".bak", backup, 0o600); err != nil {
		t.Fatalf("write existing backup: %v", err)
	}
	store, err := NewProfileStore(path)
	if err == nil {
		t.Fatal("NewProfileStore() error = nil, want backup collision")
	}
	if err := store.Add(testProfile("must-not-overwrite")); err == nil {
		t.Fatal("Add() error = nil for store with unpreserved corrupt file")
	}
	got, err := os.ReadFile(path) // #nosec G304 -- test file path is generated under t.TempDir.
	if err != nil {
		t.Fatalf("read original corrupt store: %v", err)
	}
	if string(got) != string(original) {
		t.Fatalf("original corrupt store changed to %q", got)
	}
	got, err = os.ReadFile(path + ".bak") // #nosec G304 -- backup path is derived from the generated test file path.
	if err != nil {
		t.Fatalf("read existing backup: %v", err)
	}
	if string(got) != string(backup) {
		t.Fatalf("existing backup changed to %q", got)
	}
}

func TestAddFromLinkReturnsActualDuplicateName(t *testing.T) {
	store, err := NewProfileStore(filepath.Join(t.TempDir(), "profiles.json"))
	if err != nil {
		t.Fatalf("NewProfileStore() error = %v", err)
	}
	link := "vless://" + testProfileUUID + "@proxy.example:443?type=tcp&security=none#same"
	if _, _, err := store.AddFromLink(link); err != nil {
		t.Fatalf("first AddFromLink() error = %v", err)
	}
	got, warnings, err := store.AddFromLink(link)
	if err != nil {
		t.Fatalf("second AddFromLink() error = %v", err)
	}
	if got.Name != "same" || len(warnings) != 0 || len(store.List()) != 1 {
		t.Fatalf("AddFromLink() = %q warnings=%v profiles=%d, want existing profile same",
			got.Name, warnings, len(store.List()))
	}
}

func TestAddFromSubscription(t *testing.T) {
	link := "vless://" + testProfileUUID + "@proxy.example:443?type=tcp&security=none#subscribed"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(writer, link+"\n"+link)
	}))
	defer server.Close()
	store, err := NewProfileStore(filepath.Join(t.TempDir(), "profiles.json"))
	if err != nil {
		t.Fatalf("NewProfileStore() error = %v", err)
	}
	added, err := store.AddFromSubscription(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("AddFromSubscription() error = %v", err)
	}
	if added != 1 || len(store.List()) != 1 || store.List()[0].Name != "subscribed" {
		t.Fatalf("imported profiles = %#v, added = %d", store.List(), added)
	}
}

func testProfile(name string) config.Profile {
	return config.Profile{
		Name: name, Protocol: "vless", Address: "proxy.example.com",
		Port: 443, UUID: testProfileUUID, Network: "tcp", Security: "none",
	}
}

const testProfileUUID = "01234567-89ab-cdef-0123-456789abcdef"
