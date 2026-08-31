package clipboard

import "context"

type Event struct{ Text string }
type Health struct {
	Running   bool   `json:"running"`
	LastEvent string `json:"last_event,omitempty"`
	LastError string `json:"last_error,omitempty"`
}
type Adapter interface {
	Start(context.Context) (<-chan Event, error)
	Write(context.Context, string) error
	Health() Health
	Close() error
}
