package queue

import (
	"context"

	"github.com/Witriol/my-downloader/internal/downloader"
)

// Downloader defines the minimal aria2 client surface used by the queue.
type Downloader interface {
	AddURI(ctx context.Context, uri string, options map[string]string) (string, error)
	TellStatus(ctx context.Context, gid string) (*downloader.Status, error)
	Pause(ctx context.Context, gid string) error
	Unpause(ctx context.Context, gid string) error
	Remove(ctx context.Context, gid string) error
}

var _ Downloader = (*downloader.Aria2Client)(nil)
