package qoslistener

import (
	"context"
	"golang.org/x/time/rate"
	"io"
	"math"
	"net"
	"sync/atomic"
	"time"
)

type operation int

// internal consts to easily identify two types of operations available for observation
const (
	opWrite operation = iota
	opRead
)

// qosconn struct implements net.Conn interface. Connection of qosconn type cannot be created directly
// outside of this package.
//
// It wraps other net.Conn connection that will be rate limited on Read() and Write() operations.
// Rate limiting is backed with rate.Limiter taken from "golang.org/x/time/rate". rate.Limiter implements
// token bucket algorithm.
// qosconn treats 1 token as a one available byte of bandwidth.
//
// There are two rate.Limiter instances used within each qosconn:
// - pcLimiter which is per-connection rate limiter and is independent per connection. Access here is not synchronized.
// - parent (.globalLimiter) which is per-listener rate limiter and is shared between different connections. That one is
//   synchronized in parent (instance of QoSListener)
type qosconn struct {
	watchedConn net.Conn
	parent      *QoSListener
	pcLimiter   *rate.Limiter
	pcBandwidth int32
}

func newConn(conn net.Conn, parent *QoSListener, pcBandwidth int32) *qosconn {
	return &qosconn{
		watchedConn: conn,
		parent:      parent,
		pcBandwidth: pcBandwidth,
		pcLimiter:   createNewRateLimiter(pcBandwidth),
	}
}

func createNewRateLimiter(bandwidth int32) *rate.Limiter {
	var limiter *rate.Limiter
	if bandwidth <= AllowAllTraffic {
		limiter = rate.NewLimiter(rate.Limit(math.MaxFloat64), 0)
	} else {
		limiter = rate.NewLimiter(rate.Limit(bandwidth), int(bandwidth))
	}
	return limiter
}

func (c *qosconn) Read(b []byte) (int, error) {
	return c.rateLimitOperation(b, opRead)
}

func (c *qosconn) Write(b []byte) (int, error) {
	return c.rateLimitOperation(b, opWrite)
}

func (c *qosconn) Close() error {
	return c.watchedConn.Close()
}

func (c *qosconn) LocalAddr() net.Addr {
	return c.watchedConn.LocalAddr()
}

func (c *qosconn) RemoteAddr() net.Addr {
	return c.watchedConn.RemoteAddr()
}

func (c *qosconn) SetDeadline(t time.Time) error {
	return c.watchedConn.SetDeadline(t)
}

func (c *qosconn) SetReadDeadline(t time.Time) error {
	return c.watchedConn.SetReadDeadline(t)
}

func (c *qosconn) SetWriteDeadline(t time.Time) error {
	return c.watchedConn.SetWriteDeadline(t)
}

func (c *qosconn) findBufferSize(connectionLimiter, globalLimiter *rate.Limiter, initial int) int {
	bufferSize := initial
	// get full burst per connection
	connectionLimiterFraction := connectionLimiter.Burst()
	if connectionLimiter.Limit() != rate.Inf && connectionLimiter.Burst() == 0 {
		return 0
	}
	if connectionLimiter.Limit() != rate.Inf && bufferSize > connectionLimiter.Burst() {
		bufferSize = connectionLimiterFraction
	}
	// allow to get maximum 1/1000 of listener bandwidth per single read/write request
	globalLimiterFraction := globalLimiter.Burst() / 1000
	if globalLimiter.Limit() != rate.Inf && globalLimiter.Burst() == 0 {
		return 0
	}
	if globalLimiter.Limit() != rate.Inf && bufferSize > globalLimiterFraction {
		bufferSize = globalLimiterFraction
	}
	if bufferSize == 0 {
		bufferSize = 1
	}
	return bufferSize
}

func (c *qosconn) rateLimitOperation(b []byte, op operation) (int, error) {
	var connErr error
	processed := 0
	for processed < len(b) && connErr != io.EOF {
		// verify if connection bandwidth has changed and create new rate limiter if needed
		parentBandwidth := atomic.LoadInt32(&c.parent.pcBandwidth)
		if parentBandwidth != c.pcBandwidth {
			c.pcBandwidth = parentBandwidth
			c.pcLimiter = createNewRateLimiter(c.pcBandwidth)
		}
		gl := (*rate.Limiter)(atomic.LoadPointer(&c.parent.globalLimiter))

		start := processed
		bufferSize := c.findBufferSize(c.pcLimiter, gl, len(b)-processed)
		err := c.pcLimiter.WaitN(context.Background(), bufferSize)
		if err != nil {
			return 0, err
		}
		err = gl.WaitN(context.Background(), bufferSize)
		if err != nil {
			return 0, err
		}
		var n int
		switch op {
		case opWrite:
			n, connErr = c.watchedConn.Write(b[start : start+bufferSize])
		case opRead:
			buffer := make([]byte, bufferSize)
			n, connErr = c.watchedConn.Read(buffer)
			copy(b[start:start+n], buffer[:])
		}
		if connErr != nil && connErr != io.EOF {
			return 0, connErr
		}
		processed += n
	}
	return processed, connErr
}
