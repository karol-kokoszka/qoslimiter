package qoslistener

import (
	"context"
	"io"
	"net"
	"sync/atomic"

	"golang.org/x/time/rate"
)

type operation int

// internal consts to easily identify two types of operations available for observation
const (
	opWrite operation = iota
	opRead
)

type inConn = net.Conn

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
	inConn

	parent      *QoSListener
	pcLimiter   *rate.Limiter
	pcBandwidth int32
}

func newConn(conn net.Conn, parent *QoSListener, pcBandwidth int32) *qosconn {
	return &qosconn{
		inConn:      conn,
		parent:      parent,
		pcBandwidth: pcBandwidth,
		pcLimiter:   rate.NewLimiter(findLimit(int(pcBandwidth)), findBurst(int(pcBandwidth))),
	}
}

func (c *qosconn) updateRateLimiter(bandwidth int32) {
	c.pcLimiter.SetLimit(findLimit(int(bandwidth)))
	c.pcLimiter.SetBurst(findBurst(int(bandwidth)))
}

func (c *qosconn) Read(b []byte) (int, error) {
	return c.rateLimitOperation(b, opRead)
}

func (c *qosconn) Write(b []byte) (int, error) {
	return c.rateLimitOperation(b, opWrite)
}

func (c *qosconn) findBufferSize(connectionLimiter, globalLimiter *rate.Limiter, initial int) int {
	bufferSize := initial
	connectionLimiterFraction := connectionLimiter.Burst()
	if connectionLimiter.Limit() != rate.Inf && connectionLimiter.Burst() == 0 {
		return 0
	}
	if connectionLimiter.Limit() != rate.Inf && bufferSize > connectionLimiter.Burst() {
		bufferSize = connectionLimiterFraction
	}
	globalLimiterFraction := globalLimiter.Burst() / 10000
	if globalLimiter.Limit() != rate.Inf && globalLimiter.Burst() == 0 {
		return 0
	}
	if globalLimiter.Limit() != rate.Inf && bufferSize > globalLimiterFraction {
		bufferSize = globalLimiterFraction
	}
	if bufferSize == 1 {
		bufferSize = 64
	}
	if initial < bufferSize {
		bufferSize = initial
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
			c.updateRateLimiter(c.pcBandwidth)
		}

		c.parent.lockLim()
		start := processed
		bufferSize := c.findBufferSize(c.pcLimiter, c.parent.globalLimiter, len(b)-processed)
		err := c.parent.globalLimiter.WaitN(context.Background(), bufferSize)
		if err != nil {
			return 0, err
		}
		c.parent.unlockLim()
		err = c.pcLimiter.WaitN(context.Background(), bufferSize)
		if err != nil {
			return 0, err
		}

		var n int
		switch op {
		case opWrite:
			n, connErr = c.inConn.Write(b[start : start+bufferSize])
		case opRead:
			buffer := make([]byte, bufferSize)
			n, connErr = c.inConn.Read(buffer)
			copy(b[start:start+n], buffer[:])
		}
		if connErr != nil && connErr != io.EOF {
			return 0, connErr
		}
		processed += n
	}
	return processed, connErr
}
