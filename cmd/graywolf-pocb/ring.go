package main

import "sync"

// decodedLine is the JSON-serialised view of a frame that the WebView shows.
type decodedLine struct {
	Stamp string `json:"stamp"`
	Text  string `json:"text"`
}

// frameRing keeps the most-recent N decoded frames and a lifetime count.
// Newest is index 0 in snapshot().
type frameRing struct {
	mu    sync.Mutex
	cap   int
	items []decodedLine
	total uint64
}

func newFrameRing(capacity int) *frameRing {
	return &frameRing{cap: capacity, items: make([]decodedLine, 0, capacity)}
}

func (r *frameRing) push(line decodedLine) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.total++
	r.items = append([]decodedLine{line}, r.items...)
	if len(r.items) > r.cap {
		r.items = r.items[:r.cap]
	}
}

func (r *frameRing) snapshot() []decodedLine {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]decodedLine, len(r.items))
	copy(out, r.items)
	return out
}

func (r *frameRing) count() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.total
}
