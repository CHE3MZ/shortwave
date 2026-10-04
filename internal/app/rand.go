package app

import (
	"math/rand"
	"time"
)

// shuffled returns a random permutation of servers using a local source so
// concurrent callers don't contend on the global rand lock.
func shuffled(pool []string) []string {
	r := rand.New(rand.NewSource(time.Now().UnixNano())) // #nosec G404 -- non-security server order
	order := r.Perm(len(pool))
	out := make([]string, 0, len(pool))
	for _, idx := range order {
		out = append(out, pool[idx])
	}
	return out
}
