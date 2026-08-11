package domain

// WaitingBufferPercentSource provides the current global waiting-buffer percentage.
// Implementations must be safe for concurrent reads from HTTP handlers and workers.
type WaitingBufferPercentSource interface {
	CurrentWaitingBufferPercent() int
}

type StaticWaitingBufferPercent int

func (percent StaticWaitingBufferPercent) CurrentWaitingBufferPercent() int {
	return int(percent)
}
