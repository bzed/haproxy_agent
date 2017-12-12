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

var last_health int32 = 100
var last_idle int64 = 0
var last_total int64 = 0

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
			panic("Failed to read /proc/stats")
			continue
		}
		idleTicks := float64(idle - atomic.LoadInt64(&last_idle))
		totalTicks := float64(total - atomic.LoadInt64(&last_total))
		cpuUsage := float64(0.0)
		if totalTicks > 0 {
			cpuUsage = 100 * ((totalTicks - idleTicks) / totalTicks)
		}

		haproxy_health := int32(math.Floor(100 - cpuUsage))

		// ensure we do not drain nodes...
		if haproxy_health <= 0 {
			haproxy_health = 1
		}

		atomic.StoreInt64(&last_idle, idle)
		atomic.StoreInt64(&last_total, total)
		atomic.StoreInt32(&last_health, haproxy_health)
		time.Sleep(time.Duration(milliseconds) * time.Millisecond)
	}
}

func clientConnections(listener net.Listener) chan net.Conn {
	ch := make(chan net.Conn)
	go func() {
		for {
			client, err := listener.Accept()
			if client == nil {
				log.Fatal("failed to accept: " + err.Error())
				continue
			}
			ch <- client
		}
	}()
	return ch
}

func handleConnection(client net.Conn, log_requests bool) {
	health := atomic.LoadInt32(&last_health)
	readyness := fmt.Sprintf("%d%% ready", health)
	if log_requests {
		log.Println(client.RemoteAddr(), readyness)
	}
	client.Write([]byte(readyness + "\n"))
	client.Close()
}

func main() {
	debugPort := flag.Int("port", 7777, "tcp port to use (ignored if activated via systemd)")
	timeframe := flag.Int("timeframe", 2000, "calculate cpu usage for this timeframe in milliseconds")
	log_requests := flag.Bool("log-requests", false, "log each request with remote IP and returned health")
	flag.Parse()

	var tcp_listener net.Listener
	var err error
	use_systemd := false

	systemd_listeners, err := activation.Listeners(true)
	if err != nil {
		panic("Failed to start systemd activation")
	}
	if len(systemd_listeners) != 1 {
		use_systemd = false
	}

	if use_systemd {
		log.Println("Using systemd activation")
		tcp_listener = systemd_listeners[0]
	} else {
		log.Println("Using normal tcp port")
		tcp_listener, err = net.Listen("tcp", ":"+strconv.Itoa(*debugPort))
		if tcp_listener == nil {
			panic("couldn't start normal TCP listener: " + err.Error())
		}
		log.Println("Listening on port", *debugPort)
	}

	go calculateHealth(*timeframe)

	connections := clientConnections(tcp_listener)
	for {
		go handleConnection(<-connections, *log_requests)
	}

}
