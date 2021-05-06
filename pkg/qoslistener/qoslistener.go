package qoslistener

import (
	"math"
	"net"
	"sync/atomic"
	"unsafe"

	"golang.org/x/time/rate"
)

const (
	AllowAllTraffic = -1
)

// QoSListener struct implements net.Listener interface and is the wrapper over watchedListener that allows to
// rate limit bandwidth.
//
// There are two rate.Limiter instances used by single QoSListener:
// - pcLimiter which is per-connection rate limiter and is independent per connection. Access here is not synchronized.
//   pcLimiter bandwidth size is limited to 2147483647 bytes as value is stored in int32.
// - globalLimiter which is per-listener rate limiter and is shared between different connections. That one is
//   synchronized.
//
// Example usage:
//    func myLimitedListener(l net.Listener, limitGlobal, limitPerConn int) net.Listener {
//      limited := qoslistener.NewListener(l)
//      limited.SetLimits(limitGlobal, limitPerConn)
//      return limited
//    }
type QoSListener struct {
	watchedListener net.Listener
	globalLimiter   unsafe.Pointer
	pcBandwidth     int32
}

func NewListener(listener net.Listener) *QoSListener {
	return &QoSListener{
		watchedListener: listener,
		globalLimiter:   unsafe.Pointer(rate.NewLimiter(rate.Limit(math.MaxFloat64), 0)),
		pcBandwidth:     int32(AllowAllTraffic),
	}
}

func (l *QoSListener) Accept() (net.Conn, error) {
	conn, err := l.watchedListener.Accept()
	if err != nil {
		return nil, err
	}
	return newConn(conn, l, atomic.LoadInt32(&l.pcBandwidth)), nil
}

func (l *QoSListener) Close() error {
	return l.watchedListener.Close()
}

func (l *QoSListener) Addr() net.Addr {
	return l.watchedListener.Addr()
}

// SetLimits method is exposed to allow setting and changing bandwidth limits at runtime.
// It creates new listener-limiter and saves values of connection-bytes-per-second that is shared
// between all connections.
func (l *QoSListener) SetLimits(globalBps, connectionBps int) {
	var newGlobalLimiter *rate.Limiter
	if globalBps <= AllowAllTraffic {
		newGlobalLimiter = rate.NewLimiter(rate.Limit(math.MaxFloat64), 0)
	} else {
		newGlobalLimiter = rate.NewLimiter(rate.Limit(globalBps), globalBps)
	}
	atomic.StorePointer(&l.globalLimiter, unsafe.Pointer(newGlobalLimiter))
	atomic.StoreInt32(&l.pcBandwidth, int32(connectionBps))
}
