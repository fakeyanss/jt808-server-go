package hash

import "hash/fnv"

func Hash(src string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(src))
	return h.Sum32()
}
