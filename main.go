package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"os/exec"
	"sync"
	"time"
)

func main() {
	// Parse arguments
	var start = flag.Int("start", 0, "Begin scanning from address 0")
	var step = flag.Int("step", 1, "Step this much each time - used for distributed execution")
	var randomize = flag.Bool("random", false, "Randomize the start step")
	var threads = flag.Int("threads", 1, "Processing threads per IP address")
	flag.Parse()

	// We sometimes need the random number generator seeded, even if not picking a random Start
	rand.Seed(time.Now().UnixNano())

	if *randomize {
		*start = rand.Int()
	}

	fmt.Println("Beginning scan from ", *start)

	stats := Stats{Info: make(map[string]int)}

	// Create a thread which displays output containing statistics about the run
	go showStatus(&stats)

	hosts := make(chan string)
	go generateHosts(hosts, *start, *step)

	validHosts := make(chan string)

	// WaitGroup for checkHosts workers (the only threaded component of the system)
	var wg sync.WaitGroup

	// Get our addresses
	addrs := getLocalAddresses()

	wg.Add(*threads * len(addrs))

	for _, addr := range addrs {
		for i := 0; i < *threads; i++ {
			go checkHosts(hosts, validHosts, &wg, &stats, addr)
		}
	}

	// TODO: Instead of creating a new set of network connections, we should just render the response we already got
	// However, as most ngrok tunnel URLs aren't active, there is relatively little overhead of doing it this way
	done := screenshotHosts(validHosts)

	// Wait until the checkHosts workers are done, at which point we can close the validHosts channel to signal screenshotHosts
	wg.Wait()
	close(validHosts)
	fmt.Println("All hosts processed, waiting for screenshots to complete...")

	// Wait until screenshotHosts is complete, the final step
	<-done

}

func generateHosts(hosts chan<- string, start int, step int) {
	// Create a list of every possible ngrok host
	// TODO: Do not create this list sequentially, instead randomize it
	total := 1 << 32
	lessTotal := total / step
	for i := 0; i < lessTotal; i += step {
		hostInt := (i + start) % total // Ensure we loop back around to the new/first ones
		host := fmt.Sprintf("%08x", hostInt)
		hosts <- host
	}
	fmt.Println("Host creation complete")
	close(hosts)
}

func checkHosts(hosts <-chan string, validHosts chan<- string, wg *sync.WaitGroup, stats *Stats, addr *net.IPNet) {
	for host := range hosts {
		// For each host, we need to get a new dialier with a new local address
		client := GetHttpClient(addr)
		// Test if the ngrok.io URL returns the right response
		// Use HEAD requests to save bandwidth
		resp, err := client.Head("http://" + host + ".ngrok.io/")
		if err != nil {
			// Assume this was a timeout
			// TODO: Fix this -- re-add it to the queue
			panic(err)
		}
		// Categorize it based on the response
		if resp.StatusCode == 404 && resp.ContentLength == 34 {
			// This is the case of Tunnel [xxx].ngrok.io not found
			stats.Increment("notfound")
		} else if resp.StatusCode == 502 && resp.ContentLength == 1590 {
			// This occurs when the ngrok client is running, but the port on the client end is not open
			stats.Increment("tunneldown")
			/* } else if strings.Contains(bodyStr, "This tunnel expired ") {
			// Free tunnels cannot be up for a long period, else they expire
			// This tunnel expired x days ago
			stats.Increment("expired")
			*/
		} else {
			fmt.Println("Found a host that was up!", host)
			fmt.Println(resp.StatusCode, resp.ContentLength)
			// Submit for a screenshot
			validHosts <- host
			stats.Increment("valid")
		}
		// Close out connection(s)
		//client.CloseIdleConnections()
		transport, ok := client.Transport.(*http.Transport)
		if ok {
			transport.CloseIdleConnections()
		}
	}
	wg.Done()
}

func screenshotHosts(validHosts <-chan string) chan bool {
	doneChan := make(chan bool)
	go func() {
		for validHost := range validHosts {
			// Take a screenshot of the host
			// Will output a JSON blob with url and filename
			fmt.Println("Running node")
			cmd := exec.Command("node",
				"./http-screenshotter.js",
				"/home/ubuntu/images/",
				"http", validHost+".ngrok.io", "80",
			)
			timer := time.AfterFunc(30*time.Second, func() {
				fmt.Println("Killing stalled Chrome screenshot")
				// TODO: Check error here and fix
				cmd.Process.Kill()
			})
			cmdOutput, err := cmd.Output()
			timer.Stop()
			if err != nil {
				// We just didn't save an image
				fmt.Println("No screenshot taken:", err)
				continue
			}
			var bdoc interface{}
			err = json.Unmarshal(cmdOutput, &bdoc)
			if err != nil {
				fmt.Println("Invalid JSON returned from node tool")
				fmt.Println(err)
				continue
			}
		}
		// Signal we're done to main()
		doneChan <- true
	}()
	return doneChan
}

// Object that tracks stats across the run
type Stats struct {
	mux  sync.Mutex
	Info map[string]int
}

func (i *Stats) Increment(state string) {
	i.mux.Lock()
	defer i.mux.Unlock()
	i.Info[state] = i.Info[state] + 1
}

func (i *Stats) Get() map[string]int {
	i.mux.Lock()
	defer i.mux.Unlock()
	newMap := make(map[string]int)
	for k, v := range i.Info {
		newMap[k] = v
	}
	return newMap
}

func showStatus(s *Stats) {
	fmt.Println(" Not Found | Tunnel Down | Expired | Valid | -- Speed -- ")
	total := 0
	for {
		data := s.Get()
		newTotal := data["notfound"] + data["tunneldown"] + data["expired"] + data["valid"]
		fmt.Printf("\r %09d | %011d | %07d | %05d | -- %dr/s --  ", data["notfound"], data["tunneldown"], data["expired"], data["valid"], newTotal-total)
		total = newTotal
		// Only update once per second
		time.Sleep(1 * time.Second)
	}
}

// Get local addresses
func getLocalAddresses() []*net.IPNet {
	validAddr := make([]*net.IPNet, 0)
	ifaces, err := net.Interfaces()
	if err != nil {
		panic(err)
	}
	for _, i := range ifaces {
		addrs, err := i.Addrs()
		if err != nil {
			panic(err)
		}

		for _, a := range addrs {
			switch v := a.(type) {
			// TODO: When is this IPAddr instead of IPNet?
			case *net.IPNet:
				// Valid IP address here, need to check if it's local or not though
				if isPrivateIP(v.IP) {
					continue
				}
				// It's not local, so lets add it to the list of IPs we'll use to enumerate valid tunnels
				validAddr = append(validAddr, v)
			}
		}

	}

	return validAddr
}

func isPrivateIP(ip net.IP) bool {
	privateIPBlocks := make([]*net.IPNet, 0)

	for _, cidr := range []string{
		"127.0.0.0/8",    // IPv4 loopback
		"10.0.0.0/8",     // RFC1918
		"172.16.0.0/12",  // RFC1918
		"192.168.0.0/16", // RFC1918
		"::1/128",        // IPv6 loopback
		"fe80::/10",      // IPv6 link-local
		"fc00::/7",       // IPv6 unique local addr
	} {
		_, block, _ := net.ParseCIDR(cidr)
		privateIPBlocks = append(privateIPBlocks, block)
	}

	for _, block := range privateIPBlocks {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

func GetHttpClient(addr *net.IPNet) http.Client {
	dialer := &net.Dialer{
		Timeout:   2 * time.Second,
		KeepAlive: 0,
		LocalAddr: &net.TCPAddr{
			IP:   addr.IP,
			Port: 0,
		},
	}
	tr := &http.Transport{
		Dial:                dialer.Dial,
		TLSHandshakeTimeout: 5 * time.Second,
	}
	fmt.Println(addr)
	client := http.Client{Transport: tr}
	return client
}
