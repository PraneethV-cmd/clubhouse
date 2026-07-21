// Package names makes up friendly default handles like "turbo-otter" so nobody
// has to fill in the config to get going.
package names

import (
	"fmt"
	"math/rand"
)

var (
	adjectives = []string{"turbo", "sleepy", "brave", "cosmic", "sneaky", "jolly", "witty", "spicy", "mellow", "zippy", "fuzzy", "rowdy", "cheeky", "swift", "lucky"}
	animals    = []string{"otter", "raccoon", "gecko", "panda", "walrus", "ferret", "moose", "narwhal", "koala", "badger", "lynx", "puffin", "hedgehog", "wombat", "axolotl"}
)

// Random returns a handle like "spicy-narwhal-42".
func Random() string {
	return fmt.Sprintf("%s-%s-%d", pick(adjectives), pick(animals), rand.Intn(100))
}

func pick(s []string) string { return s[rand.Intn(len(s))] }
