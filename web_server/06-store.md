# 06. ドメインとストレージ層

## この章のゴール

- ドメイン型 `Todo` を定義する
- 「永続化はインターフェイス越し」の設計を身につける
- スレッドセーフな in-memory 実装を書く
- 「本番では DB に差し替える」までを一直線でつなげる

## 6.1 なぜ Store を interface にするのか

「Todo を保存する場所」は開発時は in-memory の map で十分ですが、本番では DB や KVS になります。
最初から `map` に直接触るハンドラを書いてしまうと、後から DB 化するときに全ハンドラを直す羽目になります。

そこで **抽象** を挟みます。

```
Handler ── (Store interface) ── MemStore / SQLStore / RedisStore / ...
```

Handler は `Store` インターフェイスに対して喋る。実装は本番と開発で入れ替える。
このために **必要最小限のメソッド** を interface に切り出します。

## 6.2 `Todo` 型

```go
// todo.go
package main

import "time"

// Todo はドメインの中心となる型。JSON でクライアントに返す形はここのタグで決まる。
type Todo struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Done      bool      `json:"done"`
	CreatedAt time.Time `json:"created_at"`
}
```

- ID は `int64`。将来 DB を採番するときのために大きめの整数にしておく。
- `time.Time` は JSON では RFC 3339 形式の文字列になる（`"2026-08-18T09:00:00Z"`）。
- タグ `json:"created_at"` はスネークケースで統一。

## 6.3 `Store` インターフェイス

`store.go` を作ります。

```go
// store.go
package main

import (
	"context"
	"errors"
)

// ErrTodoNotFound は Store から Todo が見つからなかったときの sentinel error。
// ハンドラ側では errors.Is で判定して 404 に変換する。
var ErrTodoNotFound = errors.New("todo not found")

// Store は永続化層のインターフェイス。
// 実装を差し替えれば in-memory から DB に切り替えられる。
type Store interface {
	Create(ctx context.Context, title string) (Todo, error)
	List(ctx context.Context) ([]Todo, error)
	Get(ctx context.Context, id int64) (Todo, error)
	Update(ctx context.Context, id int64, title string, done bool) (Todo, error)
	Delete(ctx context.Context, id int64) error
}
```

### なぜ `ctx` が第 1 引数か

[章 05](./05-context.md) で見たとおり、クライアント切断時のキャンセル伝播のためです。
DB 実装（例: `db.QueryContext(ctx, ...)`）ではこれが必須になります。in-memory 実装では利用機会は少ないですが、**インターフェイスの契約として最初から入れる**のが正解です。

### なぜ `ErrTodoNotFound` を用意するか

- 呼び出し側は [`errors.Is(err, ErrTodoNotFound)`](https://pkg.go.dev/errors#Is) で 404 に変換する
- 「エラー文字列で判定」だとタイポや翻訳で崩壊する

sentinel error のパターンです。将来もっと種類が増えたら struct 型のエラーに拡張してもいいですが、今はこれで十分。

## 6.4 in-memory 実装

同じ `store.go` に追記します。

```go
import (
	"cmp"
	"context"
	"errors"
	"slices"
	"sync"
	"time"
)

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
```

### なぜ [`sync.RWMutex`](https://pkg.go.dev/sync#RWMutex) か

`net/http` のサーバは 1 リクエスト＝ 1 goroutine で走ります。
複数のリクエストが同時に `MemStore.Create` などを呼ぶと、Go の map は **[並行アクセスで即クラッシュ](https://go.dev/doc/faq#atomic_maps)** します。

そこで:

- **書き込み系** (`Create`, `Update`, `Delete`) は `s.mu.Lock()` で排他
- **読み取り系** (`Get`, `List`) は `s.mu.RLock()` で複数同時読み許可

[`sync.Mutex`](https://pkg.go.dev/sync#Mutex) でも動きますが、読みが多い API では `RWMutex` の方がスループットが出ます。

### 各メソッドの実装

```go
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
```

### コードの読みどころ

- **`ctx.Err()` を最初にチェック**: すでにキャンセルされたリクエストなら無駄仕事しない。DB 実装では `db.QueryContext(ctx, ...)` が同等のことをやってくれる。
- **[`time.Now().UTC()`](https://pkg.go.dev/time#Now)**: サーバ時刻は基本 UTC で保存し、表示層で現地時刻に直す。混在するとバグの温床。
- **[`slices.SortFunc`](https://pkg.go.dev/slices#SortFunc) + [`cmp.Compare`](https://pkg.go.dev/cmp#Compare)**: Go 1.21+ のイディオム。[`sort.Slice`](https://pkg.go.dev/sort#Slice) の後継として推奨。
- **`nextID`**: 単調増加。in-memory 実装なのでプロセス再起動で 1 に戻る（教材前提）。本番では DB のシーケンス or UUID に置き換える。

## 6.5 Store をハンドラから使う準備

次章で `Handler` を書きますが、そこでは

```go
h.store.Create(r.Context(), title)
```

のように呼びます。**`Store` インターフェイスに対して呼ぶ**ので、`Handler` は `MemStore` の実在を知りません。

`main.go` では

```go
store := NewMemStore()          // ← ここで具体実装を注入
handler := NewHandler(store, logger)
```

とすることで、テストでは `MemStore` を、将来は `SQLStore` を渡せる形になります。

## 次の章

[07. ハンドラの実装 →](./07-handlers.md)
