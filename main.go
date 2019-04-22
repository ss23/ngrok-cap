package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
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
	var threads = flag.Int("threads", 1, "Processing threads")
	flag.Parse()

	if *randomize {
		rand.Seed(time.Now().UnixNano())
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

	wg.Add(*threads)
	for i := 0; i < *threads; i++ {
		go checkHosts(hosts, validHosts, &wg, &stats)
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

func checkHosts(hosts <-chan string, validHosts chan<- string, wg *sync.WaitGroup, stats *Stats) {
	for host := range hosts {
		// Test if the ngrok.io URL returns the right response
		// Use HEAD requests to save bandwidth
		resp, err := http.Head("http://" + host + ".ngrok.io/")
		if err != nil {
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
				"http", validHost + ".ngrok.io", "80",
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
