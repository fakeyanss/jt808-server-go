package hash

import "hash/fnv"

// Return 32-bit hash result.
func FNV32(src string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(src))
	return h.Sum32()
}
