package testenv

import (
	"context"
	"net"
	"time"

	"golang.org/x/time/rate"
)

// ThrottledDialer implements anacrolix/torrent's dialer.T (registered via
// Client.AddDialer) so outgoing peer connections pay artificial latency and
// bandwidth costs. Tests otherwise run over loopback, which has neither, so
// benchmarks against a loopback seeder measure distribyted's own overhead
// rather than how it behaves against a real, slow, high-RTT peer.
type ThrottledDialer struct {
	// Latency is added once, before the TCP handshake, to approximate RTT.
	Latency time.Duration
	// BytesPerSecond caps sustained throughput in each direction. Zero means
	// unlimited (latency only).
	BytesPerSecond int
}

func (d ThrottledDialer) DialerNetwork() string { return "tcp" }

func (d ThrottledDialer) Dial(ctx context.Context, addr string) (net.Conn, error) {
	if d.Latency > 0 {
		select {
		case <-time.After(d.Latency):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	if d.BytesPerSecond <= 0 {
		return conn, nil
	}

	// Burst must cover the largest single Read/Write the caller makes (bittorrent
	// chunks are 16KiB; leave generous headroom for handshake/bitfield messages
	// that don't chunk), but must NOT scale up with BytesPerSecond: rate.Limiter
	// lets a full burst through instantly regardless of the configured rate, so
	// burst == BytesPerSecond (a full second's allowance) let any transfer
	// smaller than one second's worth of bandwidth bypass throttling entirely
	// (e.g. an 8MB transfer against a 12MB/s cap measured ~70MB/s, unthrottled).
	// Capping burst independently of the rate keeps that window small regardless
	// of how high BytesPerSecond is.
	const minBurst = 64 * 1024
	const maxBurst = 256 * 1024
	burst := d.BytesPerSecond
	if burst > maxBurst {
		burst = maxBurst
	} else if burst < minBurst {
		burst = minBurst
	}
	return &throttledConn{
		Conn:     conn,
		readLim:  rate.NewLimiter(rate.Limit(d.BytesPerSecond), burst),
		writeLim: rate.NewLimiter(rate.Limit(d.BytesPerSecond), burst),
	}, nil
}

type throttledConn struct {
	net.Conn
	readLim  *rate.Limiter
	writeLim *rate.Limiter
}

func (c *throttledConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		_ = c.readLim.WaitN(context.Background(), n)
	}
	return n, err
}

func (c *throttledConn) Write(p []byte) (int, error) {
	if err := c.writeLim.WaitN(context.Background(), len(p)); err != nil {
		return 0, err
	}
	return c.Conn.Write(p)
}
