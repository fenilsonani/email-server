package imap

import (
	"log"
	"math"
)

func safeMessageCount(count int) uint32 {
	if count < 0 {
		return 0
	}
	if count > math.MaxUint32 {
		log.Printf("IMAP: clamping message count %d to uint32 max", count)
		return math.MaxUint32
	}
	return uint32(count)
}
