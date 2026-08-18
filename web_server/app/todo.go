package main

import "time"

// Todo はドメインの中心となる型。JSON でクライアントに返す形はここのタグで決まる。
type Todo struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Done      bool      `json:"done"`
	CreatedAt time.Time `json:"created_at"`
}
