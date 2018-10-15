package swarm

import "hash/fnv"

// HashPeer hashes a peer address into a Raft ID.
func HashPeer(peer string) uint64 {
	h := fnv.New64()
	h.Write([]byte(peer))
	return h.Sum64()
}
