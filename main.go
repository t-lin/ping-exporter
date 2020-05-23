/* MIT License
 *
 * Copyright (c) 2020 Thomas Lin
 * Copyright (c) 2016 Cameron Sparr
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in all
 * copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
*/
package main

/*
Based upon:
- github.com/sparrc/go-ping
- github.com/paihu/netflow_exporter
*/

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/sparrc/go-ping"
)

var usage = `
Usage:

    ping [-bind-addr listen-address] [-c count] [-i interval] [-t timeout] host
`

func main() {
	pTimeout := flag.Duration("t", time.Hour*876000, "Timeout to wait before program exits.")
	pInterval := flag.Duration("i", time.Second, "Interval between ICMP requests.")
	pCount := flag.Int("c", -1, "Number of ICMP requests to send, defaults to infinity.")
	pMetricsPath := flag.String("metrics-path", "/metrics", "Path under which to expose Prometheus metrics.")
	pListenAddress := flag.String("bind-addr", ":9999", "Address on which to expose metrics.")

	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Println(usage)
		return
	}

	hostname, err := os.Hostname()
	if err != nil {
		fmt.Println("Unable to get hostname")
		fmt.Println(err)
		return
	}

	// Set up Prometheus GaugeVec object
	pingGaugeVec := promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ping_rtt",
			Help: "Historical ping RTTs over time (ms)",
		},
		[]string{
			"targetHost", // Specify ping target
			"hostname",   // Name of host running ping-exporter
		},
	)

	// Map Prometheus metrics scrape path to handler function
	http.Handle(*pMetricsPath, promhttp.Handler())

	// Parse target host and create Pinger object
	targetHost := flag.Arg(0)
	pinger, err := ping.NewPinger(targetHost)
	if err != nil {
		fmt.Printf("ERROR: %s\n", err.Error())
		return
	}

	// Listen for interrupt signal (SIGINT), i.e. Ctrl+C and stop pinger
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for _ = range c {
			pinger.Stop()
		}
	}()

	// Get Gauge object with targetHost
	pingGauge := pingGaugeVec.WithLabelValues(targetHost, hostname)

	// Define OnRecv function for receiving ICMPs => Update gauge
	pinger.OnRecv = func(pkt *ping.Packet) {
		pingGauge.Set(float64(pkt.Rtt) / 1000000) // Convert to ns to ms
		fmt.Printf("%d bytes from %s: icmp_seq=%d time=%v\n",
			pkt.Nbytes, pkt.IPAddr, pkt.Seq, pkt.Rtt)
	}

	// Stats function when ping ends
	pinger.OnFinish = func(stats *ping.Statistics) {
		fmt.Printf("\n--- %s ping statistics ---\n", stats.Addr)
		fmt.Printf("%d packets transmitted, %d packets received, %v%% packet loss\n",
			stats.PacketsSent, stats.PacketsRecv, stats.PacketLoss)
		fmt.Printf("round-trip min/avg/max/stddev = %v/%v/%v/%v\n",
			stats.MinRtt, stats.AvgRtt, stats.MaxRtt, stats.StdDevRtt)
	}

	pinger.Count = *pCount
	pinger.Interval = *pInterval
	pinger.Timeout = *pTimeout
	pinger.SetPrivileged(true)

	// Start server in separate goroutine
	go http.ListenAndServe(*pListenAddress, nil)
	fmt.Printf("Now listening on %s\n", *pListenAddress)

	fmt.Printf("PING %s (%s):\n", pinger.Addr(), pinger.IPAddr())
	pinger.Run() // Blocking

	return
}
