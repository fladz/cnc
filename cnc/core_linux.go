// +build linux

package main

import (
	"context"
	"fmt"
	"github.com/c9s/goprocinfo/linux"
	"log"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

var traceroute, ping, mtr string

const (
	numPing = 15
	procCPU = "/proc/cpuinfo"
	procMem = "/proc/meminfo"
)

// func checkStart {{{

func checkStart() {
	// Check if commands are installed. If not, we need to find alternatives.
	ping = findCommand("ping")
	traceroute = findCommand("traceroute")
	mtr = findCommand("mtr")

	// Run various tests.
	getIPISP()
	getOSInfo()
	getCPUInfo()
	getMemInfo()
	if ping != "" {
		runPing()
	}
	if traceroute != "" {
		runTraceroute()
	} else if mtr != "" {
		runMTR()
	}
	testDownload()
} // }}}

// func findCommand {{{

func findCommand(cmd string) string {
	// Set a command's full path just in case it's installed in
	// not-default location.
	path, err := exec.LookPath(cmd)
	if err != nil {
		// Not found, let's set to null.
		return ""
	}
	return path
} // }}}

// func getOSInfo {{{

func getOSInfo() {
	fmt.Println("\nObtaining OS info...")
	log.Println("\n---- OS Info ----")

	var osinfo syscall.Utsname
	if err := syscall.Uname(&osinfo); err != nil {
		log.Printf("  ERROR: Cannot obtain OS info: %s\n", err)
		return
	}

	results.OS = OSJSON{
		Name:    intArrayToString(osinfo.Sysname),
		Version: intArrayToString(osinfo.Version),
		Kernel:  intArrayToString(osinfo.Release),
		Model:   intArrayToString(osinfo.Machine),
	}

	log.Printf("  Name   : %s\n", results.OS.Name)
	log.Printf("  Release: %s\n", results.OS.Kernel)
	log.Printf("  Version: %s\n", results.OS.Version)
	log.Printf("  Machine: %s\n", results.OS.Model)
} // }}}

// func intArrayToString {{{

func intArrayToString(in [65]int8) string {
	var buf [65]byte
	for i, b := range in {
		buf[i] = byte(b)
	}
	str := string(buf[:])
	if i := strings.Index(str, "\x00"); i != -1 {
		str = str[:i]
	}
	return str
} // }}}

// func getCPUInfo {{{

func getCPUInfo() {
	fmt.Println("\nObtaining CPU info...")
	log.Println("\n---- CPU Info ----")

	cpu, err := linux.ReadCPUInfo(procCPU)
	if err != nil {
		log.Printf("  ERROR: %s\n", err)
		return
	}

	results.CPU.Number = len(cpu.Processors)
	log.Printf("  Total Number of CPU Cores: %d\n", results.CPU.Number)

	for i, p := range cpu.Processors {
		results.CPU.CPU = append(results.CPU.CPU, p.ModelName)
		log.Printf("    [%d]: %s\n", i+1, p.ModelName)
	}
} // }}}

// func getMemInfo {{{

func getMemInfo() {
	fmt.Println("\nObtaining memory info...")
	log.Println("\n---- Memory Info ----")

	mem, err := linux.ReadMemInfo(procMem)
	if err != nil {
		log.Printf("  ERROR: %s\n", err)
		return
	}

	results.Mem = MemJSON{
		Physical: Mem{
			Total: uint64(mem.MemTotal / 1024),
			Free:  uint64(mem.MemFree / 1024),
			Used:  uint64((mem.MemTotal - mem.MemFree) / 1024),
		},
		Virtual: Mem{
			Total: uint64(mem.SwapTotal / 1024),
			Free:  uint64(mem.SwapFree / 1024),
			Used:  uint64((mem.SwapTotal - mem.SwapFree) / 1024),
		},
	}

	log.Printf("  Physical Total: %d MB\n", results.Mem.Physical.Total)
	log.Printf("  Physical Free : %d MB\n", results.Mem.Physical.Free)
	log.Printf("  Physical Used : %d MB\n", results.Mem.Physical.Used)
	log.Printf("  Virtual Total : %d MB\n", results.Mem.Virtual.Total)
	log.Printf("  Virtual Free  : %d MB\n", results.Mem.Virtual.Free)
	log.Printf("  Virtual Used  : %d MB\n", results.Mem.Virtual.Used)
} // }}}

// func runMTR {{{

func runMTR() {
	results.Trace = make([]ExecJSON, 0)
	var err error

	for _, dest := range netDestinations {
		fmt.Printf("\nRunning trace (%s)...\n", dest)
		log.Printf("\n\n---- MTR (%s) ----\n", dest)
		r := ExecJSON{Location: dest}

		// mtr is significantly slower than traceroute,
		// so let's set longer deadline.
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(3*time.Minute))
		defer cancel()

		cmd := exec.CommandContext(ctx, mtr, "-r", "-c", "1", dest)
		if r.Output, err = execCommand(cmd); err != nil {
			log.Printf("  ERROR: Running mtr to %s failed (%s)\n", dest, err)
		}

		// Save in our result set.
		results.Trace = append(results.Trace, r)
	}
} // }}}
