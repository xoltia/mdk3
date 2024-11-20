package queue

import (
	crand "crypto/rand"
	_ "embed"
	"encoding/json"
	"math/rand/v2"
)

//go:embed slugs.json
var slugsData []byte

var (
	slugs []string
	zipf  *rand.Zipf
)

func init() {
	if err := json.Unmarshal(slugsData, &slugs); err != nil {
		panic(err)
	}

	var seed [32]byte
	if _, err := crand.Read(seed[:]); err != nil {
		panic(err)
	}
	SeedSlugGenerator(seed)
}

func randomSlug() string {
	return slugs[zipf.Uint64()]
}

func SeedSlugGenerator(seed [32]byte) {
	s := rand.NewChaCha8(seed)
	r := rand.New(s)
	zipf = rand.NewZipf(r, 1.1, 36.5, uint64(len(slugs)-1))
}
