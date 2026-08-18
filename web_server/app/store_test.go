package main

import (
	"context"
	"errors"
	"testing"
)

func TestMemStore_CRUD(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()

	// 空リスト
	got, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List on empty: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty list, got %d", len(got))
	}

	// Create
	a, err := s.Create(ctx, "buy milk")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if a.ID == 0 || a.Title != "buy milk" || a.Done {
		t.Fatalf("unexpected created todo: %+v", a)
	}

	b, err := s.Create(ctx, "walk the dog")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if b.ID != a.ID+1 {
		t.Fatalf("expected sequential ids, got %d after %d", b.ID, a.ID)
	}

	// List: 2件、ID昇順
	got, err = s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 || got[0].ID != a.ID || got[1].ID != b.ID {
		t.Fatalf("unexpected list: %+v", got)
	}

	// Get
	one, err := s.Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if one.Title != "buy milk" {
		t.Fatalf("wrong todo returned: %+v", one)
	}

	// Update
	upd, err := s.Update(ctx, a.ID, "buy oat milk", true)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !upd.Done || upd.Title != "buy oat milk" {
		t.Fatalf("update did not apply: %+v", upd)
	}

	// Delete
	if err := s.Delete(ctx, b.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, b.ID); !errors.Is(err, ErrTodoNotFound) {
		t.Fatalf("expected ErrTodoNotFound, got %v", err)
	}
}

func TestMemStore_NotFound(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()
	if _, err := s.Get(ctx, 999); !errors.Is(err, ErrTodoNotFound) {
		t.Fatalf("expected ErrTodoNotFound, got %v", err)
	}
	if err := s.Delete(ctx, 999); !errors.Is(err, ErrTodoNotFound) {
		t.Fatalf("expected ErrTodoNotFound, got %v", err)
	}
	if _, err := s.Update(ctx, 999, "x", false); !errors.Is(err, ErrTodoNotFound) {
		t.Fatalf("expected ErrTodoNotFound, got %v", err)
	}
}

func TestMemStore_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 即キャンセル
	s := NewMemStore()
	if _, err := s.Create(ctx, "x"); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
