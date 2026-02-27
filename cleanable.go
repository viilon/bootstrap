package bootstrap

// Cleanable is the interface that groups the basic Cleanup method.
type Cleanable interface {
	Cleanup() error
}
