package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/cavaliercoder/grab"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// func listenSignal {{{

func listenSignal() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT, syscall.SIGKILL, os.Interrupt)
	go func() {
		_ = <-sig
		fmt.Println("Shutdown signal received, exiting")
		log.Println("Shutdown signal received")
		// If a test file is partially/fully downloaded, remove it before exiting.
		var path string
		for _, src := range downloadTestSources {
			if dir != "" {
				path = dir + "/" + filepath.Base(src.url)
			} else {
				path = filepath.Base(src.url)
			}
			os.Remove(path)
		}
		os.Exit(1)
	}()
} // }}}

// func getIPISP {{{

func getIPISP() {
	fmt.Println("\nObtaining client IP info...")
	log.Println("\n---- Client IP Info ----")

	res, err := http.Get(ispEndpoint)
	if err != nil {
		log.Printf("  ERROR: %s\n", err)
		return
	}
	defer res.Body.Close()

	var resp IPJSON
	if err = json.NewDecoder(res.Body).Decode(&resp); err != nil {
		log.Printf("  ERROR: %s\n", err)
		return
	}

	log.Printf("  Client IP=%s, ISP=%s, CountryCode=%s, City=%s\n",
		resp.IP, resp.ISP, resp.Country, resp.City)

	// Save in result struct.
	results.IP = resp
} // }}}

// func runPing {{{

func runPing() {
	// Init ping test results.
	results.Ping = make([]ExecJSON, 0)
	var err error

	for _, dest := range netDestinations {
		r := ExecJSON{Location: dest}
		fmt.Printf("\nRunning ping (%s)...\n", dest)
		log.Printf("\n\n---- Ping (%s) ----\n", dest)

		var cmd *exec.Cmd
		var ctx context.Context
		var cancel context.CancelFunc
		if numPing > 0 {
			// Set command timeout based on number of packets.
			// Set 2sec per packet.
			ctx, cancel = context.WithTimeout(context.Background(), time.Duration(time.Duration(numPing)*time.Second*2))

			// Number of ping packets defined, use it.
			cmd = exec.CommandContext(ctx, ping, "-c", strconv.Itoa(numPing), dest)
		} else {
			// Set command timeout - 1 minute total.
			ctx, cancel = context.WithTimeout(context.Background(), time.Duration(30*time.Second))

			// No ping packet limit, use default.
			cmd = exec.CommandContext(ctx, ping, dest)
		}
		defer cancel()

		if r.Output, err = execCommand(cmd); err != nil {
			log.Printf("  ERROR: Running ping to %s failed (%s)\n", dest, err)
		}

		// Save the result in result set.
		results.Ping = append(results.Ping, r)
	}
} // }}}

// func runTraceroute {{{

func runTraceroute() {
	// Init test results.
	results.Trace = make([]ExecJSON, 0)
	var err error

	for _, dest := range netDestinations {
		r := ExecJSON{Location: dest}
		fmt.Printf("\nRunning traceroute (%s)...\n", dest)
		log.Printf("\n\n---- Traceroute (%s) ----\n", dest)

		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(time.Duration(hops)*time.Second*2))
		defer cancel()

		cmd := exec.CommandContext(ctx, traceroute, dest)
		if r.Output, err = execCommand(cmd); err != nil {
			log.Printf("  ERROR: Running traceroute to %s failed (%s)\n", dest, err)
		}

		// Save result in result set.
		results.Trace = append(results.Trace, r)
	}
} // }}}

// func execCommand {{{

func execCommand(cmd *exec.Cmd) ([]string, error) {
	var line string
	var lines []string

	// Prep stdout stream
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	// Prep stream scanner for inputs
	scanner := bufio.NewScanner(pipe)

	// Print out stdout input as it comes in
	go func() {
		for scanner.Scan() {
			if len(scanner.Text()) == 0 {
				continue
			}
			line = fmt.Sprintf("  %s", scanner.Text())
			lines = append(lines, line)
			log.Print(line)
		}
		if err = scanner.Err(); err != nil {
			// If the error is about reading from a closed file descriptor,
			// it's ok to ignore as we're exiting this loop.
			if err != os.ErrClosed {
				e := fmt.Sprintf("  ERROR: %s", err)
				lines = append(lines, e)
				log.Println(e)
			}
		}
	}()

	if err = cmd.Start(); err != nil {
		return nil, err
	}

	if err = cmd.Wait(); err != nil {
		// We got an error somewhere during the process,
		// return output we have so far, along with the error.
		return lines, err
	}

	return lines, nil
} // }}}

// func testDownload {{{

func testDownload() {
	// Init download result struct.
	results.Download = make([]DownloadJSON, 0)

	for _, src := range downloadTestSources {
		fmt.Printf("\nRunning download test (%s, %s)... This could take a while depends on your Internet speed, please be patient\n", src.label, src.url)
		log.Printf("\n---- Download Test (%s, %s) ----\n", src.label, src.url)

		runDownload(src)
	}
} // }}}

// func runDownload {{{

func runDownload(src *downloadTestSource) {
	// Init download result for this location.
	r := DownloadJSON{Location: src.label, File: src.url}

	// Set timeout so we don't wait forever
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	req, err := grab.NewRequest(".", src.url)
	if err != nil {
		log.Printf("  ERROR: Prepping download request failed - %s\n", err)
		return
	}
	req = req.WithContext(ctx)

	// Send the request and print out progress
	res := grab.DefaultClient.Do(req)
	defer os.Remove(res.Filename)

	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()

Loop:
	for {
		select {
		case <-tick.C:
			log.Printf("  transferred %v / %v bytes (%.2f%%)\n",
				res.BytesComplete(), res.Size, 100*res.Progress())
		case <-res.Done:
			break Loop
		}
	}

	// Final check - did everything go ok?
	if err = res.Err(); err != nil {
		log.Printf("  ERROR: Download failed - %s\n", err)
		return
	}

	if res.HTTPResponse.StatusCode != http.StatusOK {
		log.Printf("  ERROR: Download failed - %s\n", res.HTTPResponse.Status)
		return
	}

	// Calculate download speed
	// Mbps: megabits per second
	mbps := float64((res.Size)*8/1000000) / float64(res.Duration().Seconds())

	// Save calculated speed in result.
	r.Speed = fmt.Sprintf("%.2fMbps", mbps)

	log.Printf("  Download Size : %d bytes\n", res.Size)
	log.Printf("  Download Time : %s\n", res.Duration())
	log.Printf("  Download Speed: %s\n", r.Speed)

	// Save the result in result set.
	results.Download = append(results.Download, r)
} // }}}
