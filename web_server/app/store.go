package main

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"sync"
	"time"
)

// ErrTodoNotFound は Store から Todo が見つからなかったときの sentinel error。
// ハンドラ側では errors.Is で判定して 404 に変換する。
var ErrTodoNotFound = errors.New("todo not found")

// Store は永続化層のインターフェイス。
// 実装を差し替えれば in-memory から DB に切り替えられる。
// 第 1 引数の ctx はキャンセル・タイムアウトを伝播させるための約束。
type Store interface {
	Create(ctx context.Context, title string) (Todo, error)
	List(ctx context.Context) ([]Todo, error)
	Get(ctx context.Context, id int64) (Todo, error)
	Update(ctx context.Context, id int64, title string, done bool) (Todo, error)
	Delete(ctx context.Context, id int64) error
}

// MemStore は Store の in-memory 実装。
// sync.RWMutex でマップを保護する。
type MemStore struct {
	mu     sync.RWMutex
	nextID int64
	todos  map[int64]Todo
}

func NewMemStore() *MemStore {
	return &MemStore{todos: make(map[int64]Todo)}
}

func (s *MemStore) Create(ctx context.Context, title string) (Todo, error) {
	if err := ctx.Err(); err != nil {
		return Todo{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	t := Todo{
		ID:        s.nextID,
		Title:     title,
		CreatedAt: time.Now().UTC(),
	}
	s.todos[t.ID] = t
	return t, nil
}

func (s *MemStore) List(ctx context.Context) ([]Todo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Todo, 0, len(s.todos))
	for _, t := range s.todos {
		out = append(out, t)
	}
	// ID 昇順で安定した順序に整える。
	slices.SortFunc(out, func(a, b Todo) int { return cmp.Compare(a.ID, b.ID) })
	return out, nil
}

func (s *MemStore) Get(ctx context.Context, id int64) (Todo, error) {
	if err := ctx.Err(); err != nil {
		return Todo{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.todos[id]
	if !ok {
		return Todo{}, ErrTodoNotFound
	}
	return t, nil
}

func (s *MemStore) Update(ctx context.Context, id int64, title string, done bool) (Todo, error) {
	if err := ctx.Err(); err != nil {
		return Todo{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.todos[id]
	if !ok {
		return Todo{}, ErrTodoNotFound
	}
	t.Title = title
	t.Done = done
	s.todos[id] = t
	return t, nil
}

func (s *MemStore) Delete(ctx context.Context, id int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.todos[id]; !ok {
		return ErrTodoNotFound
	}
	delete(s.todos, id)
	return nil
}

