package llm

import (
	"context"
	"errors"
	"fmt"
)

// ErrUnavailable marks a configured provider that cannot serve requests
// (for example its stored credential could not be decrypted). It stays in
// the registry under its own name so nodes that select it fail with this
// reason instead of being routed to some other provider.
var ErrUnavailable = errors.New("llm: provider unavailable")

type unavailable struct {
	name string
	err  error
}

// Unavailable returns a provider that answers every call with
// ErrUnavailable wrapping reason.
func Unavailable(name string, reason error) Provider {
	return &unavailable{name: name, err: fmt.Errorf("%w: %s: %v", ErrUnavailable, name, reason)}
}

func (u *unavailable) Name() string { return u.name }

func (u *unavailable) Chat(context.Context, *ChatRequest) (*ChatResponse, error) { return nil, u.err }

func (u *unavailable) Models(context.Context) ([]ModelInfo, error) { return nil, u.err }

// UnavailableReason returns the error of a provider built by Unavailable,
// or nil for a working provider.
func UnavailableReason(p Provider) error {
	if u, ok := p.(*unavailable); ok {
		return u.err
	}
	return nil
}
