package main

import "time"

const (
	txQueueSize       = 1024 // capacity of async TX ring
	serialReadBufSize = 4096 // per read() buffer for serial backend
	// largeBufferReclaimThreshold is the capacity above which the temporary
	// serial RX accumulation buffer is discarded and reallocated once empty.
	// This prevents pathological growth (e.g., after bursts of noise / junk)
	// from permanently retaining large backing arrays. 16 KiB chosen as a
	// balance: comfortably larger than typical aggregated frame bursts yet
	// small enough to free memory if garbage accumulates.
	largeBufferReclaimThreshold = 16 * 1024 // reclaim serial accumulator if grown beyond this and fully drained
	rxBackoffMin                = 20 * time.Millisecond
	rxBackoffMax                = 500 * time.Millisecond
)
