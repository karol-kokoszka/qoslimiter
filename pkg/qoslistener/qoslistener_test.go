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
			// consumes whole data from the connection in 512kB portions
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
		<-ctx.Done()
		done = true
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

	t.Parallel()

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
			connBandwidth:     10 * kilobyte,
			listenerBandwidth: 5 * megabyte,
			expectedBandwidth: 200 * kilobyte,
			dataSize:          2 * kilobyte,
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
					<-ctx.Done()
					done = true
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

	t.Parallel()

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
			noConn:            300,
			connBandwidth:     10 * megabyte,
			listenerBandwidth: 128 * megabyte,
			expectedBandwidth: 128 * megabyte,
			dataSize:          512 * kilobyte,
			description: `[30sec long] bandwidth should be equal to +/- 5% 128MB/sec when connBandwidth=10MB, listenerBandwidth=128MB,
		numberOfConnections=300`,
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
					<-ctx.Done()
					done = true
				}()
				for !done {
					conn, err := qosListener.Accept()
					if err != nil {
						panic(err)
					}
					go func() {
						var connErr error
						var n int
						readBuffer := make([]byte, kilobyte)
						for connErr != io.EOF {
							n, connErr = conn.Read(readBuffer)
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

func TestQoSListener_Accept_ChangeLimitsAtRuntime(t *testing.T) {

	type Step struct {
		measurementPeriod time.Duration
		connBandwidth     int
		listenerBandwidth int
	}

	type TestCase struct {
		noConn            int
		steps             []*Step
		expectedBandwidth float64
		dataSize          int
		description       string
	}

	tcs := []*TestCase{
		{
			noConn:            20,
			expectedBandwidth: 20 * megabyte,
			dataSize:          32 * kilobyte,
			steps: []*Step{
				{
					measurementPeriod: 30 * time.Second,
					connBandwidth:     megabyte,
					listenerBandwidth: 50 * megabyte,
				},
				{
					measurementPeriod: 30 * time.Second,
					connBandwidth:     0,
					listenerBandwidth: 0,
				},
				{
					measurementPeriod: 30 * time.Second,
					connBandwidth:     4 * megabyte,
					listenerBandwidth: 40 * megabyte,
				},
			},
			description: `[90sec long] bandwidth should be equal to +/- 20% 20MB/sec with 3 30sec periods 20MB/sec, 0MB/sec, 40MB/sec`,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.description, func(t *testing.T) {
			data := generateXSizeLongBytesData(tc.dataSize)

			rawListener, err := net.Listen("tcp4", ":0")
			require.NoError(t, err)
			qosListener := NewListener(rawListener)
			qosListener.SetLimits(tc.steps[0].listenerBandwidth, tc.steps[0].connBandwidth)

			go startConsumer(qosListener.Addr().String(), tc.noConn)
			written := uint64(0)

			ctx, cancel := context.WithCancel(context.Background())
			go func(ctx context.Context) {
				defer qosListener.Close()

				done := false
				go func() {
					<-ctx.Done()
					done = true
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
			executionTime := tc.steps[0].measurementPeriod.Seconds()
			time.Sleep(tc.steps[0].measurementPeriod)
			for i := 1; i < len(tc.steps); i++ {
				qosListener.SetLimits(tc.steps[i].listenerBandwidth, tc.steps[i].connBandwidth)
				executionTime += tc.steps[i].measurementPeriod.Seconds()
				time.Sleep(tc.steps[i].measurementPeriod)
			}
			// then
			cancel()
			realBw := float64(written) / executionTime
			tolerance := 0.2 * tc.expectedBandwidth

			fmt.Println(tc.description)
			fmt.Println((written/kilobyte)/uint64(executionTime), " kB/sec")
			assert.True(t, math.Abs(tc.expectedBandwidth-realBw) < tolerance,
				fmt.Sprintf("Expected %f +/- %f, but was %f", tc.expectedBandwidth, tolerance, realBw))
		})
	}

}
