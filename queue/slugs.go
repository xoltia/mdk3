package queue

import (
	_ "embed"
	"encoding/json"
	"math/rand"
	"time"
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

	s := rand.NewSource(time.Now().UnixNano())
	r := rand.New(s)
	zipf = rand.NewZipf(r, 1.1, 36.5, uint64(len(slugs)-1))
}

func randomSlug() string {
	return slugs[zipf.Uint64()]
}
