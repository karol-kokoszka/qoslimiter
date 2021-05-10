# **qoslimiter v0.1**

QoSListener allows to create quality-of-service over any existing net.Listener.
The main purpose of this library is to rate limit TCP bandwidth on the listener level and on the single connection level as well.
**SLA** for 30s transfer sample consumed bandwidth should be accurate +/- 5%

Implementation is backed with [token bucket algorithm](https://en.wikipedia.org/wiki/Token_bucket) provided in [pkg.go.dev/golang.org/x/time/rate](https://pkg.go.dev/golang.org/x/time/rate#section-documentation). One token reflects one byte of bandwidth.

Example usage:
```
func myLimitedListener(l net.Listener, limitGlobal, limitPerConn int) net.Listener {
      limited := qoslistener.NewListener(l)
      limited.SetLimits(limitGlobal, limitPerConn)
      return limited
}
```

- **limitGlobal** parameter defines bytes/sec available for the whole listener
- **limitPerConn** parameter defines bytes/sec available for a single connection handled by the listener.

---
**Makefile** targets:
- **deps** - to get dependencies
- **test** - to execute tests
- **docker-build** - to build docker image used for executing tests
- **docker-test** - to execute tests in separate environment (please use this goal for executing tests and veryfing solution)

Tests for the library are long-running ones and a single test cases takes 30s to complete. 
