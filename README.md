# **qoslimiter v0.1**

QoSListener allows to create quality-of-service over any existing net.Listener.
The main purpose of this library is to rate limit TCP bandwidth on the listener level and on the single connection level as well.

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

Tests for the library are long-running ones and a single test cases takes 30s to complete. 
