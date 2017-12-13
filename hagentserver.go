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
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
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

func calculateHealth(milliseconds int) {
	for {
		idle, total, err := getCPUSample()
		if err != nil {
			log.Fatal("Failed to read /proc/stats")
			continue
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
		time.Sleep(time.Duration(milliseconds) * time.Millisecond)
	}
}

func clientConnections(listener net.Listener) chan net.Conn {
	ch := make(chan net.Conn)
	go func() {
		for {
			client, err := listener.Accept()
			if client == nil {
				log.Fatalf("Failed to accept: %s", err.Error())
				continue
			}
			ch <- client
		}
	}()
	return ch
}

func handleConnection(client net.Conn, logRequests bool) {
	health := atomic.LoadInt32(&lastHealth)
	readyness := fmt.Sprintf("%d%% ready", health)
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

func main() {
	debugPort := flag.Int("port", 7777, "tcp port to use (ignored if activated via systemd)")
	timeframe := flag.Int("timeframe", 2000, "calculate cpu usage for this timeframe in milliseconds")
	logRequests := flag.Bool("log-requests", false, "log each request with remote IP and returned health")
	flag.Parse()

	var tcpListener net.Listener
	var err error
	useSystemd := false

	systemdListeners, err := activation.Listeners(true)
	if err != nil {
		log.Fatal("Failed to start systemd activation")
	}
	if len(systemdListeners) != 1 {
		useSystemd = false
	}

	if useSystemd {
		log.Println("Using systemd activation")
		tcpListener = systemdListeners[0]
	} else {
		log.Println("Using normal tcp port")
		tcpListener, err = net.Listen("tcp", ":"+strconv.Itoa(*debugPort))
		if tcpListener == nil {
			log.Fatalf("Couldn't start a normal TCP listener: %s", err.Error())
		}
		log.Printf("Listening on port %d\n", *debugPort)
	}

	go calculateHealth(*timeframe)

	connections := clientConnections(tcpListener)
	for {
		go handleConnection(<-connections, *logRequests)
	}

}
