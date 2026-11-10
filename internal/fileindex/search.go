package fileindex

import (
	"bytes"
	"container/heap"
	"context"
	"runtime"
	"slices"
	"sort"
	"sync"
)

// hit is a matching entry and its score.
type hit struct {
	v     *volume
	i     int
	score int
}

// top keeps the best hits seen, as a min-heap on score.
type top struct {
	hits []hit
	n    int
}

func (t *top) Len() int           { return len(t.hits) }
func (t *top) Less(a, b int) bool { return t.hits[a].score < t.hits[b].score }
func (t *top) Swap(a, b int)      { t.hits[a], t.hits[b] = t.hits[b], t.hits[a] }
func (t *top) Push(x any)         { t.hits = append(t.hits, x.(hit)) }
func (t *top) Pop() any {
	h := t.hits[len(t.hits)-1]
	t.hits = t.hits[:len(t.hits)-1]
	return h
}

func (t *top) offer(h hit) {
	switch {
	case len(t.hits) < t.n:
		heap.Push(t, h)
	case h.score > t.hits[0].score:
		t.hits[0] = h
		heap.Fix(t, 0)
	}
}

// checkEvery is how many entries or hits a search goes through between
// looks at whether it was cancelled.
const checkEvery = 1 << 14

// search finds the best limit entries of the volumes that match q, best
// first, and returns their paths. The volumes are read-locked while
// searched.
func search(ctx context.Context, vols []*volume, q query, limit int) []string {
	if len(q.groups) == 0 || limit <= 0 {
		return nil
	}
	best := &top{n: limit}
	for _, v := range vols {
		v.mu.RLock()
		for _, h := range searchVolume(ctx, v, q, limit) {
			best.offer(h)
		}
		if ctx.Err() != nil {
			v.mu.RUnlock()
			return nil
		}
		v.mu.RUnlock()
	}
	hits := best.hits
	slices.SortFunc(hits, func(a, b hit) int { return b.score - a.score })
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		h.v.mu.RLock()
		p, ok := h.v.path(h.i, false)
		h.v.mu.RUnlock()
		if ok {
			out = append(out, p)
		}
	}
	return out
}

// searchVolume splits the volume's entries among the processors.
func searchVolume(ctx context.Context, v *volume, q query, limit int) []hit {
	n := len(v.off)
	if n == 0 {
		return nil
	}
	workers := min(runtime.GOMAXPROCS(0), max(1, n/50000))
	results := make([]*top, workers)
	var wg sync.WaitGroup
	for w := range workers {
		lo, hi := n*w/workers, n*(w+1)/workers
		results[w] = &top{n: limit}
		wg.Go(func() { scanRange(ctx, v, q, lo, hi, results[w]) })
	}
	wg.Wait()
	var all []hit
	for _, t := range results {
		all = append(all, t.hits...)
	}
	return all
}

// scanRange tests entries lo to hi. With an anchor word, it looks for the
// word in the names as one block, which is much faster than
// testing each name, and tests only the entries it is found in.
func scanRange(ctx context.Context, v *volume, q query, lo, hi int, out *top) {
	test := func(i int) {
		// An excluded folder stays only to recognise changes inside it.
		if v.flags[i]&(flagDead|flagExcluded) != 0 {
			return
		}
		c := candidate{v: v, i: i}
		if !q.match(&c) {
			return
		}
		// Depth only lowers a score, so a hit that cannot make the top
		// even at the root is skipped before walking up its folders.
		if len(out.hits) == out.n && q.score(v, i, 0) <= out.hits[0].score {
			return
		}
		depth, ok := v.depth(i)
		if !ok {
			return
		}
		out.offer(hit{v: v, i: i, score: q.score(v, i, depth)})
	}
	if q.anchor == "" {
		for i := lo; i < hi; i++ {
			if (i-lo)%checkEvery == 0 && ctx.Err() != nil {
				return
			}
			test(i)
		}
		return
	}
	anchor := []byte(q.anchor)
	end := len(v.names)
	if hi < len(v.off) {
		end = int(v.off[hi])
	}
	pos, seen := int(v.off[lo]), 0
	for pos < end {
		k := bytes.Index(v.names[pos:end], anchor)
		if k < 0 {
			return
		}
		at := pos + k
		// The entry whose name holds the match: the last one starting at
		// or before it.
		i := lo + sort.Search(hi-lo, func(j int) bool { return int(v.off[lo+j]) > at }) - 1
		test(i)
		// Go on after this entry's name.
		pos = end
		if i+1 < len(v.off) {
			pos = min(end, int(v.off[i+1]))
		}
		if seen++; seen%checkEvery == 0 && ctx.Err() != nil {
			return
		}
	}
}
