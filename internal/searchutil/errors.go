package searchutil

import (
	"context"
	"errors"
	"fmt"
	"net"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var ErrIndexUnavailable = errors.New("retrieval index unavailable")

func IndexUnavailable(err error) error {
	return fmt.Errorf("%w: %v", ErrIndexUnavailable, err)
}

// ClassifyIndexError marks only transport failures and explicit missing/unavailable
// index responses as eligible for the bounded database fallback.
func ClassifyIndexError(err error) error {
	if err == nil || errors.Is(err, ErrIndexUnavailable) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return IndexUnavailable(err)
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.NotFound:
		return IndexUnavailable(err)
	default:
		return err
	}
}
