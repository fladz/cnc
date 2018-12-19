// +build windows

package main

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	ping       = "ping"
	traceroute = "tracert"
	sysinfo    = "systeminfo"
	find       = "findstr"
	numPing    = 0
)

// func checkStart {{{

func checkStart() {
	// Run various tests.
	getIPISP()
	getSystemInfo()
	runPing()
	runTraceroute()
	testDownload()
} // }}}

// func getSystemInfo {{{

func getSystemInfo() {
	fmt.Println("\nObtaining System Info...")
	log.Println("\n---- System Info ----")

	// Set command deadline.
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(3*time.Minute))
	defer cancel()

	// Prep commands.
	out, err := exec.CommandContext(ctx, sysinfo).Output()
	if err != nil {
		log.Printf("  ERROR: %s\n", err)
		return
	}

	// Go through output and print out what we need to know.
	var key, val string
	var isCPU bool
	var items []string

	lines := strings.Split(string(out), "\r\n")
	for _, line := range lines {
		// Output lines could be -
		//
		// KEY: VALUE
		//
		// or
		//
		// KEY: KEY: VALUE
		items = strings.Split(string(line), ":")
		if len(items) == 2 {
			// This is the
			//  KEY: VALUE
			// line.
			key = items[0]
			val = items[1]
		} else if len(items) == 3 {
			// This is the
			//  KEY: KEY: VALUE
			// line.
			key = items[0] + ":" + items[1]
			val = items[2]
		} else {
			// We don't really care this line, but just in case set the value.
			val = items[0]
		}

		// Modify values; it could have leading space(s) and tab(s).
		val = strings.TrimLeft(val, " ")
		val = strings.Replace(val, "\t", "", -1)
		val = strings.Replace(val, "\n", "", -1)

		// If key is about cpu, set the flag.
		// This is so we know next line(s) is about cpu details.
		if key == "Processor(s)" {
			isCPU = true
			// Value should be "N Processor(s) Installed."
			tmp := strings.TrimRight(val, " Processor(s) Installed.")
			if results.CPU.Number, err = strconv.Atoi(tmp); err != nil {
				log.Printf("  ERROR: Invalid # of cpu: %q\n", val)
				return
			}
			continue
		}

		// Check values of keys we care.
		switch key {
		case "OS Name":
			isCPU = false
			results.OS.Name = val
		case "OS Version":
			isCPU = false
			results.OS.Version = val
		case "OS Build Type":
			isCPU = false
			results.OS.Build = val
		case "OS Configuration":
			isCPU = false
			results.OS.Model = val
		case "Total Physical Memory":
			isCPU = false
			// Make sure the value is numeric. This is used to calculate
			// used memory.
			if results.Mem.Physical.Total, err = convertWinByteStringToUint64(val); err != nil {
				log.Printf("  ERROR: Invalid physical total memory: %q\n", val)
				return
			}
		case "Available Physical Memory":
			isCPU = false
			if results.Mem.Physical.Free, err = convertWinByteStringToUint64(val); err != nil {
				log.Printf("  ERROR: Invalid physical avail memory: %q\n", val)
				return
			}
		case "Virtual Memory: Max Size":
			isCPU = false
			if results.Mem.Virtual.Total, err = convertWinByteStringToUint64(val); err != nil {
				log.Printf("  ERROR: Invalid virtual total memory: %q\n", val)
				return
			}
		case "Virtual Memory: Available":
			isCPU = false
			if results.Mem.Virtual.Free, err = convertWinByteStringToUint64(val); err != nil {
				log.Printf("  ERROR: Invalid virtual avail memory: %q\n", val)
				return
			}
		case "Virtual Memory: In Use":
			isCPU = false
			if results.Mem.Virtual.Used, err = convertWinByteStringToUint64(val); err != nil {
				log.Printf("  ERROR: Invalid virtual used memory: %q\n", val)
				return
			}
		default:
			if isCPU {
				// If this is cpu detail, the key/val should look like -
				// Key "  \t[NUM]"
				// Val "   _MODEL_OF_CPU_"
				key = strings.Replace(key, "\t", "", 1)
				key = strings.TrimLeft(key, " ")

				// To run a sanity check, make sure the number in the key is within
				// number of installed CPUs.
				key = strings.TrimLeft(key, "[")
				key = strings.TrimRight(key, "]")
				tmp, err := strconv.Atoi(key)
				if err != nil {
					// This is not the line we care.
					continue
				}

				if results.CPU.Number >= tmp {
					results.CPU.CPU = append(results.CPU.CPU, val)
					continue
				}

				isCPU = false
				continue
			}
			isCPU = false
		}
	}

	// Ok now print out the result.
	log.Println("\n---- OS Info ----")
	log.Printf("  Name         : %s\n", results.OS.Name)
	log.Printf("  Version      : %s\n", results.OS.Version)
	log.Printf("  Build Type   : %s\n", results.OS.Build)
	log.Printf("  Configuration: %s\n", results.OS.Model)

	log.Println("\n---- CPU Info ----")
	log.Printf("  Total Number of installed CPUs: %d\n", results.CPU.Number)
	for i, c := range results.CPU.CPU {
		log.Printf("    [%d] %s\n", i, c)
	}

	log.Println("\n---- Memory Info ----")
	// Calculate physical used space. This is not retrived from sysinfo.
	results.Mem.Physical.Used = results.Mem.Physical.Total - results.Mem.Physical.Free
	log.Printf("  Physical Total: %d MB\n", results.Mem.Physical.Total)
	log.Printf("  Physical Free : %d MB\n", results.Mem.Physical.Free)
	log.Printf("  Physical Used : %d MB\n", results.Mem.Physical.Used)
	log.Printf("  Virtual Total : %d MB\n", results.Mem.Virtual.Total)
	log.Printf("  Virtual Free  : %d MB\n", results.Mem.Virtual.Free)
	log.Printf("  Virtual Used  : %d MB\n", results.Mem.Virtual.Used)
} // }}}

// func convertWinByteStringToUint64 {{{

func convertWinByteStringToUint64(in string) (uint64, error) {
	// The line should look like "X,XXX MB"
	in = strings.TrimRight(in, " MB")
	in = strings.Replace(in, ",", "", -1)
	return strconv.ParseUint(in, 10, 64)
} // }}}
