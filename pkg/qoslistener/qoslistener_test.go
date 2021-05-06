package qoslistener

import (
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	megabyte = 1024 * 1024
	kilobyte = 1024
)

func startConsumer(addr string, noConnections int) {
	for i := 0; i < noConnections; i++ {
		conn, err := net.Dial("tcp4", addr)
		if err != nil {
			panic(err)
		}
		go func() {
			// consumes whole data from the connection in 64kB portions
			var connErr error
			buffer := make([]byte, 512*kilobyte)
			for connErr != io.EOF {
				_, connErr = conn.Read(buffer)
				if connErr != nil && connErr != io.EOF {
					panic(err)
				}
			}
		}()
	}
}

func startProducer(ctx context.Context, addr string, noConnections int, data []byte) {
	done := false
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		select {
		case <-ctx.Done():
			done = true
			wg.Done()
		}
	}()

	for i := 0; i < noConnections; i++ {
		conn, err := net.Dial("tcp4", addr)
		if err != nil {
			panic(err)
		}
		go func() {
			defer conn.Close()

			for !done {
				_, err := conn.Write(data)
				if err != nil {
					panic(err)
				}
			}
		}()
	}
	wg.Wait()
}

func generateXSizeLongBytesData(size int) []byte {
	var packet []byte
	for i := 0; i < size; i++ {
		packet = append(packet, []byte(strconv.Itoa(i%10))...)
	}
	return packet
}

func TestQoSListener_Accept_MeasureProducer(t *testing.T) {

	type TestCase struct {
		measurementPeriod time.Duration
		noConn            int
		connBandwidth     int
		listenerBandwidth int
		expectedBandwidth float64
		dataSize          int
		description       string
	}

	tcs := []*TestCase{
		{
			measurementPeriod: 30 * time.Second,
			noConn:            20,
			connBandwidth:     megabyte,
			listenerBandwidth: 50 * megabyte,
			expectedBandwidth: 20 * megabyte,
			dataSize:          32 * kilobyte,
			description: `[30sec long] bandwidth should be equal to +/- 5% 20MB/sec when connBandwidth=1MB, listenerBandwidth=50MB,
		numberOfConnections=20`,
		},
		{
			measurementPeriod: 30 * time.Second,
			noConn:            20,
			connBandwidth:     20 * megabyte,
			listenerBandwidth: 50 * megabyte,
			expectedBandwidth: 50 * megabyte,
			dataSize:          512 * kilobyte,
			description: `[30sec long] bandwidth should be equal to +/- 5% 50MB/sec when connBandwidth=20MB, listenerBandwidth=50MB,
		numberOfConnections=20`,
		},
		{
			measurementPeriod: 30 * time.Second,
			noConn:            1000,
			connBandwidth:     8 * kilobyte,
			listenerBandwidth: 128 * kilobyte,
			expectedBandwidth: 128 * kilobyte,
			dataSize:          128,
			description: `[30sec long] bandwidth should be equal to +/- 5% 512kB/sec when connBandwidth=8kB, listenerBandwidth=128kB,
		numberOfConnections=1000`,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.description, func(t *testing.T) {
			data := generateXSizeLongBytesData(tc.dataSize)

			rawListener, err := net.Listen("tcp4", ":0")
			require.NoError(t, err)
			qosListener := NewListener(rawListener)
			qosListener.SetLimits(tc.listenerBandwidth, tc.connBandwidth)

			go startConsumer(qosListener.Addr().String(), tc.noConn)
			written := uint64(0)

			ctx, cancel := context.WithCancel(context.Background())
			go func(ctx context.Context) {
				defer qosListener.Close()

				done := false
				go func() {
					select {
					case <-ctx.Done():
						done = true
					}
				}()
				for !done {
					conn, err := qosListener.Accept()
					if err != nil {
						panic(err)
					}
					go func() {
						defer conn.Close()

						for !done {
							n, err := conn.Write(data)
							if err != nil {
								panic(err)
							}
							atomic.AddUint64(&written, uint64(n))
						}
					}()
				}
			}(ctx)
			// when
			time.Sleep(tc.measurementPeriod)
			// then
			cancel()
			realBw := float64(written) / tc.measurementPeriod.Seconds()
			tolerance := 0.05 * tc.expectedBandwidth

			fmt.Println(tc.description)
			fmt.Println((written/kilobyte)/30, " kB/sec")
			assert.True(t, math.Abs(tc.expectedBandwidth-realBw) < tolerance,
				fmt.Sprintf("Expected %f +/- %f, but was %f", tc.expectedBandwidth, tolerance, realBw))
		})
	}

}

func TestQoSListener_Accept_MeasureConsumer(t *testing.T) {

	type TestCase struct {
		measurementPeriod time.Duration
		noConn            int
		connBandwidth     int
		listenerBandwidth int
		expectedBandwidth float64
		dataSize          int
		description       string
	}

	tcs := []*TestCase{
		{
			measurementPeriod: 30 * time.Second,
			noConn:            1,
			connBandwidth:     megabyte,
			listenerBandwidth: 50 * megabyte,
			expectedBandwidth: megabyte,
			dataSize:          128 * kilobyte,
			description: `[30sec long] bandwidth should be equal to +/- 5% 1MB/sec when connBandwidth=1MB, listenerBandwidth=50MB,
		numberOfConnections=1`,
		},
		{
			measurementPeriod: 30 * time.Second,
			noConn:            100,
			connBandwidth:     16 * kilobyte,
			listenerBandwidth: megabyte,
			expectedBandwidth: megabyte,
			dataSize:          8 * kilobyte,
			description: `[30sec long] bandwidth should be equal to +/- 5% 1MB/sec when connBandwidth=16kB, listenerBandwidth=1MB,
		numberOfConnections=100`,
		},
		{
			measurementPeriod: 30 * time.Second,
			noConn:            2,
			connBandwidth:     -1,
			listenerBandwidth: 2 * megabyte,
			expectedBandwidth: 2 * megabyte,
			dataSize:          128 * kilobyte,
			description: `[30sec long] bandwidth should be equal to +/- 5% 2MB/sec when connBandwidth=INF, listenerBandwidth=2MB,
		numberOfConnections=2`,
		},
		{
			measurementPeriod: 30 * time.Second,
			noConn:            100,
			connBandwidth:     0,
			listenerBandwidth: 2 * megabyte,
			expectedBandwidth: 0,
			dataSize:          128 * kilobyte,
			description: `[30sec long] block the traffic when connBandwidth=0, listenerBandwidth=2MB,
		numberOfConnections=100`,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.description, func(t *testing.T) {
			data := generateXSizeLongBytesData(tc.dataSize)

			rawListener, err := net.Listen("tcp4", ":0")
			require.NoError(t, err)
			qosListener := NewListener(rawListener)
			qosListener.SetLimits(tc.listenerBandwidth, tc.connBandwidth)

			ctx, cancel := context.WithCancel(context.Background())
			go startProducer(ctx, qosListener.Addr().String(), tc.noConn, data)

			read := uint64(0)
			go func(ctx context.Context) {
				defer qosListener.Close()

				done := false
				go func() {
					select {
					case <-ctx.Done():
						done = true
					}
				}()
				for !done {
					conn, err := qosListener.Accept()
					if err != nil {
						panic(err)
					}
					go func() {
						var connErr error
						var n int
						for connErr != io.EOF {
							n, connErr = conn.Read(data)
							if connErr != nil && connErr != io.EOF {
								panic(connErr)
							}
							atomic.AddUint64(&read, uint64(n))
						}
					}()
				}
			}(ctx)
			// when
			time.Sleep(tc.measurementPeriod)
			// then
			cancel()
			realBw := float64(read) / tc.measurementPeriod.Seconds()
			tolerance := 0.05 * tc.expectedBandwidth

			fmt.Println(tc.description)
			fmt.Println((read/kilobyte)/30, " kB/sec")
			assert.True(t, math.Abs(tc.expectedBandwidth-realBw) <= tolerance,
				fmt.Sprintf("Expected %f +/- %f, but was %f", tc.expectedBandwidth, tolerance, realBw))
		})
	}

}
