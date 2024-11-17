package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"
)

//go:embed slugs.json
var slugsData []byte
var slugs []string

func init() {
	if err := json.Unmarshal(slugsData, &slugs); err != nil {
		panic(err)
	}
}

type slugGenerator struct {
	count map[string]int
	max   map[string]int
	zipf  *rand.Zipf
}

func newSlugGenerator() *slugGenerator {
	s := rand.NewSource(time.Now().UnixNano())
	r := rand.New(s)
	return &slugGenerator{
		zipf:  rand.NewZipf(r, 1.1, 36.5, uint64(len(slugs))),
		count: make(map[string]int),
		max:   make(map[string]int),
	}
}

func (g *slugGenerator) next() string {
	slug := slugs[g.zipf.Uint64()]
	id := g.increment(slug)
	if id > 0 {
		return fmt.Sprintf("%s-%d", slug, id)
	} else {
		return slug
	}
}

func (g *slugGenerator) increment(slug string) (sequenceID int) {
	g.count[slug]++
	if g.count[slug] > 1 {
		g.max[slug]++
	}
	return g.max[slug]
}

func (g *slugGenerator) decrement(slugWithSeq string) {
	end := strings.IndexByte(slugWithSeq, '-')
	if end == -1 {
		end = len(slugWithSeq)
	}
	slug := slugWithSeq[:end]
	g.count[slug]--
	if g.count[slug] == 0 {
		delete(g.max, slug)
		delete(g.count, slug)
	} else if g.count[slug] < 0 {
		panic("negative count")
	}
}
