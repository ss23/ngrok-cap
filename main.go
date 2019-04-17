package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
)

func main() {
	hosts := make(chan string)
	go generateHosts(hosts)

	validHosts := make(chan string)
	go checkHosts(hosts, validHosts)

	// TODO: Instead of creating a new set of network connections, we should just render the response we already got
	// However, as most ngrok tunnel URLs aren't active, there is relatively little overhead of doing it this way
	done := screenshotHosts(validHosts)
	<-done

}

func generateHosts(hosts chan<- string) {
	// Create a list of every possible ngrok host
	// TODO: Do not create this list sequentially, instead randomize it
	for i := 0; i < ((2 << 32) - 1); i += 1 {
		host := fmt.Sprintf("%08x", i)
		hosts <- host
	}
	fmt.Println("Host creation complete")
	close(hosts)
}

func checkHosts(hosts <-chan string, validHosts chan<- string) {
	stats := make(map[string]int)

	fmt.Println("Not found - Tunnel Down - Valid")

	for host := range hosts {
		// Test if the ngrok.io URL returns the right response
		resp, err := http.Get("http://" + host + ".ngrok.io/")
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)
		bodyStr := string(body)
		// Categorize it based on the response
		if strings.HasPrefix(bodyStr, "Tunnel ") {
			// This is the case of Tunnel [xxx].ngrok.io not found
			stats["notfound"] = stats["notfound"] + 1
		} else if strings.HasPrefix(bodyStr, "was successfully tunneled to your ngrok client,") {
			stats["tunneldown"] = stats["tunneldown"] + 1
		} else {
			fmt.Println("Found a host that was up!", host)
			// Submit for a screenshot
			validHosts <- host
			stats["valid"] = stats["valid"] + 1
		}

		// Print stats
		fmt.Printf("\r %04d %04d %04d", stats["notfound"], stats["tunneldown"], stats["valid"])
	}
	close(validHosts)
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
				"http", validHost, "80",
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
				return nil
			}
			err = json.Unmarshal(cmdOutput, &bdoc)
			if err != nil {
				fmt.Println("Invalid JSON returned from node tool")
				fmt.Println(err)
				return nil
			}
		}
		// Signal we're done to main()
		doneChan <- true
	}()
	return doneChan
}
