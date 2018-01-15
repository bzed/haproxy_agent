/*
Copyright 2017 Bernd Zeimetz <bernd@bzed.de>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

*/

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/coreos/go-systemd/activation"
)

var lastHealth int32 = 100
var lastIdle int64
var lastTotal int64

func getCPUSample() (idle, total int64, err error) {
	idle = 0
	total = 0
	var contents []byte
	var val uint64

	contents, err = ioutil.ReadFile("/proc/stat")
	if err != nil {
		return
	}
	lines := strings.Split(string(contents), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if fields[0] == "cpu" {
			numFields := len(fields)
			for i := 1; i < numFields; i++ {
				val, err = strconv.ParseUint(fields[i], 10, 64)
				if err != nil {
					fmt.Println("Error: ", i, fields[i], err)
					return
				}
				total += int64(val) // tally up all the numbers to get total ticks
				if i == 4 {         // idle is the 5th field in the cpu line
					idle = int64(val)
				}
			}
			return
		}
	}
	return
}

func updateHealthMetrics() error {
	idle, total, err := getCPUSample()
	if err != nil {
		return errors.New("Failed to read /proc/stats")
	}
	idleTicks := float64(idle - atomic.LoadInt64(&lastIdle))
	totalTicks := float64(total - atomic.LoadInt64(&lastTotal))
	cpuUsage := float64(0.0)
	if totalTicks > 0 {
		cpuUsage = 100 * ((totalTicks - idleTicks) / totalTicks)
	}

	haproxyHealth := int32(math.Floor(100 - cpuUsage))

	// ensure we do not drain nodes...
	if haproxyHealth <= 0 {
		haproxyHealth = 1
	}

	atomic.StoreInt64(&lastIdle, idle)
	atomic.StoreInt64(&lastTotal, total)
	atomic.StoreInt32(&lastHealth, haproxyHealth)
	return nil
}

func calculateHealth(ctx context.Context, milliseconds int) {
	ticker := time.NewTicker(time.Duration(milliseconds) * time.Millisecond)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if err := updateHealthMetrics(); err != nil {
			log.Fatalf("Failed to update metrics: %s", err.Error())
		}
	}
}

func clientConnections(ctx context.Context, listener net.Listener) chan net.Conn {
	ch := make(chan net.Conn)
	go func() {
		for {
			select {
			case <-ctx.Done():
				defer close(ch)
				return
			default:
			}
			client, err := listener.Accept()
			if err != nil {
				log.Fatalf("Failed to accept: %s", err.Error())
				continue
			}
			ch <- client
		}
	}()
	return ch
}

func buildStatusMessage() string {
	health := atomic.LoadInt32(&lastHealth)
	return fmt.Sprintf("%d%% ready", health)
}

func handleConnection(client net.Conn, logRequests bool) {
	readyness := buildStatusMessage()
	if logRequests {
		log.Println(client.RemoteAddr(), readyness)
	}
	if _, err := client.Write([]byte(readyness + "\n")); err != nil {
		log.Printf("Failed to write to connection: %s", err.Error())
	}
	if err := client.Close(); err != nil {
		log.Printf("Failed to close client connection: %s", err.Error())
	}
}

func runTCPListener(listener net.Listener, logRequests bool, refreshInterval int) error {
	sigChan := make(chan os.Signal, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	defer func() {
		wg.Wait()
		close(sigChan)
	}()

	signal.Notify(sigChan, syscall.SIGINT)

	wg.Add(1)
	go func() {
		_, ok := <-sigChan
		if !ok {
			return
		}
		log.Println("SIGINT received. Exiting...")
		cancel()
		wg.Done()
	}()

	wg.Add(1)
	go func() {
		calculateHealth(ctx, refreshInterval)
		wg.Done()
	}()

	connections := clientConnections(ctx, listener)
connectionLoop:
	for {
		select {
		case <-ctx.Done():
			break connectionLoop
		case conn, open := <-connections:
			if !open {
				break connectionLoop
			}
			wg.Add(1)
			go func() {
				handleConnection(conn, logRequests)
				wg.Done()
			}()
		}
	}
	return ctx.Err()
}

func main() {
	var useSystemd bool
	var debugPort int
	var logRequests bool
	var timeframe int

	flag.BoolVar(&useSystemd, "systemd", false, "Use systemd activation mechanism")
	flag.IntVar(&debugPort, "port", 0, "tcp port to use (ignored if activated via systemd)")
	flag.IntVar(&timeframe, "timeframe", 2000, "calculate cpu usage for this timeframe in milliseconds")
	flag.BoolVar(&logRequests, "log-requests", false, "log each request with remote IP and returned health")
	flag.Parse()

	if useSystemd && debugPort != 0 {
		log.Fatal("-systemd and -port are mutually exclusive.")
	}

	var tcpListener net.Listener
	var err error

	if useSystemd {
		systemdListeners, err := activation.Listeners(true)
		if err != nil {
			log.Fatal("Failed to start systemd activation")
		}
		if len(systemdListeners) != 1 {
			log.Fatal("No systemd activation listeners available")
		}
		log.Println("Using systemd activation")
		tcpListener = systemdListeners[0]
	}

	if debugPort != 0 {
		tcpListener, err = net.Listen("tcp", ":"+strconv.Itoa(debugPort))
		if tcpListener == nil {
			log.Fatalf("Couldn't start a normal TCP listener: %s", err.Error())
		}
		log.Printf("Listening on port %d\n", debugPort)
	}

	// If we are operating as a server, we continually have to handle incoming
	// connections and update the health status.
	if useSystemd || debugPort != 0 {
		if err := runTCPListener(tcpListener, logRequests, timeframe); err != nil {
			if err != context.Canceled {
				log.Fatalf("TCP listener failed: %s", err.Error())
			}
		}
		tcpListener.Close()
		return
	}

	// In the one-shot mode, we just update the metrics and print them to
	// STDOUT.
	if err := updateHealthMetrics(); err != nil {
		log.Fatalf("Failed to update metrics: %s", err.Error())
	}
	fmt.Println(buildStatusMessage())
}
