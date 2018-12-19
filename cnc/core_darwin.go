// +build darwin

package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	traceroute = "traceroute"
	ping       = "ping"
	numPing    = 15
	// Commands to get necessary values.
	sysProf   = "system_profiler"
	sysProfHW = "SPHardwareDataType"
	sysProfSW = "SPSoftwareDataType"
	vmStat    = "vm_stat"
	sysctl    = "sysctl"
	memsize   = "hw.memsize"
)

// func checkStart {{{

func checkStart() {
	// Run various tests.
	// Good items
	getIPISP()
	getSysProf()
	getMemInfo()

	runPing()
	runTraceroute()
	testDownload()
} // }}}

// func getSysProf {{{

// Call system_profiler to get device-specific values.
func getSysProf() {
	fmt.Println("\nObtaining OS info...")
	log.Println("\n---- OS Info ----")
	var values = make(map[string]string)

	// Get hardware info.
	if err := runSysProf(sysProfHW, values); err != nil {
		log.Printf("  ERROR: %s\n", err)
		return
	}

	// Get software info.
	if err := runSysProf(sysProfSW, values); err != nil {
		log.Printf("  ERROR: %s\n", err)
		return
	}

	// Save values in struct.
	results.OS = OSJSON{
		Name:    values["machine_name"],
		Version: values["os_version"],
		Kernel:  values["kernel_version"],
		Model:   values["machine_model"],
	}
	n, err := strconv.Atoi(values["number_processors"])
	if err == nil {
		results.CPU.Number = n
	}
	results.CPU.CPU = append(results.CPU.CPU, values["cpu_type"])
	results.CPU.Speed = values["current_processor_speed"]

	// Print out the result.
	log.Printf("  Name          : %s\n", results.OS.Name)
	log.Printf("  Model         : %s\n", results.OS.Model)
	log.Printf("  OS Version    : %s\n", results.OS.Version)
	log.Printf("  Kernel Version: %s\n", results.OS.Kernel)

	log.Printf("\n---- CPU Info ----\n")
	log.Printf("  Total Number of CPU Cores: %d\n", results.CPU.Number)
	log.Printf("  Processor Speed          : %s\n", results.CPU.Speed)
	log.Printf("  Processor Name           : %s\n", strings.Join(results.CPU.CPU, "\n"))
} // }}}

// func runSysProf {{{

func runSysProf(dataType string, values map[string]string) error {
	// Set command deadline.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	out, err := exec.CommandContext(ctx, sysProf, "-xml", dataType).Output()
	if err != nil {
		return err
	}

	var profData struct {
		Array struct {
			Dict struct {
				Data []byte `xml:",innerxml"`
			} `xml:"dict"`
		} `xml:"array"`
	}

	if err = xml.Unmarshal(out, &profData); err != nil {
		return err
	}

	if err = parsePlistDict(profData.Array.Dict.Data, values); err != nil {
		return err
	}

	return nil
} // }}}

// func getMemInfo {{{

func getMemInfo() {
	fmt.Println("\nObtaining memory info...")
	log.Println("\n---- Memory Info ----")

	// Get total memory first.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, sysctl, "-e", memsize).Output()
	if err != nil {
		log.Printf("  ERROR: %s\n", err)
		return
	}

	// The value should be in this format:
	// hw.memsize={byte_count}
	items := strings.Split(string(out), "=")
	if len(items) != 2 {
		log.Println("  ERROR: Invalid line, cannot obtain total memory size")
		return
	}
	total, err := strconv.ParseInt(strings.Replace(items[1], "\n", "", 1), 10, 64)
	if err != nil {
		log.Printf("  ERROR: Cannot obtain total memory size - %s\n", err)
		return
	}

	// Get vm_stat output.
	if out, err = exec.Command(vmStat).Output(); err != nil {
		log.Printf("  ERROR: Cannot obtain output from vm_stat - %s\n", err)
		return
	}

	var size, active, spec, free, wmem, swapIn, swapOut int64
	var val string

	lines := strings.Split(string(out), "\n")

	for _, line := range lines {
		// Page size is in a line with a different format than others.
		// The line *MUST* say "(page size of XXX bytes)"
		if strings.Contains(line, "page size of") {
			if size, err = getPageSize(line); err != nil {
				log.Printf("  ERROR(page size): %s\n", err)
				return
			}
			continue
		}
		// Other lines should look like this:
		// {item_key}:  {page_count}.
		if items = strings.Split(line, ":"); len(items) != 2 {
			continue
		}

		// Since we split the line with ":", the value would include paddings
		// and a trailing period.
		val = strings.Replace(items[1], " ", "", -1)
		val = strings.Replace(val, ".", "", 1)

		// Get values for keys we care.
		switch strings.ToLower(items[0]) {
		case "pages free":
			if free, err = strconv.ParseInt(val, 10, 64); err != nil {
				log.Printf("  ERROR(pages free): %s\n", err)
				return
			}
		case "pages active":
			if active, err = strconv.ParseInt(val, 10, 64); err != nil {
				log.Printf("  ERROR(pages active): %s\n", err)
				return
			}
		case "pages speculative":
			if spec, err = strconv.ParseInt(val, 10, 64); err != nil {
				log.Printf("  ERROR(pages speculative): %s\n", err)
				return
			}
		case "pages wired down":
			if wmem, err = strconv.ParseInt(val, 10, 64); err != nil {
				log.Printf("  ERROR(pages wired down): %s\n", err)
				return
			}
		case "swapins":
			if swapIn, err = strconv.ParseInt(val, 10, 64); err != nil {
				log.Printf("  ERROR(swapins): %s\n", err)
				return
			}
		case "swapouts":
			if swapOut, err = strconv.ParseInt(val, 10, 64); err != nil {
				log.Printf("  ERROR(swapouts): %s\n", err)
				return
			}
		}
	}

	// We need page size to cauculate.
	if size == 0 {
		return
	}

	// Now do the calculation and dump out.
	results.Mem = MemJSON{
		Physical: Mem{
			Total: uint64(total / 1024 / 1024),
			Free:  uint64(free * size / 1024 / 1024),
			Used:  uint64((active + spec) * size / 1024 / 1024),
		},
		Virtual: Mem{
			Total: uint64((swapIn * size / 1024 / 1024) + (swapOut * size / 1024)),
			Free:  uint64(swapOut * size / 1024),
			Used:  uint64(swapIn * size / 1024 / 1024),
		},
	}

	log.Printf("  Physical Total: %d MB\n", results.Mem.Physical.Total)
	log.Printf("  Physical Free : %d MB\n", results.Mem.Physical.Free)
	log.Printf("  Physical Used : %d MB\n", results.Mem.Physical.Used)
	log.Printf("  Physical Wired: %d MB\n", wmem*size/1024/1024)
	log.Printf("  Swap Out      : %d MB\n", results.Mem.Virtual.Free)
	log.Printf("  Swap In       : %d MB\n", results.Mem.Virtual.Used)
} // }}}

// func getPageSize {{{

func getPageSize(in string) (int64, error) {
	firstStrings := "(page size of "
	lastStrings := " bytes)"
	first := strings.Index(in, firstStrings)
	if first == -1 {
		return 0, errors.New("Invalid line, cannot obtain page size")
	}

	last := strings.Index(in, lastStrings)
	if last == -1 {
		return 0, errors.New("Invalid line, cannot obtain page size")
	}

	firstbytes := first + len(firstStrings)
	if firstbytes >= last {
		return 0, errors.New("Invalid line, cannot obtain page size")
	}

	// Ok now parse the byte string into numeric and return.
	return strconv.ParseInt(in[firstbytes:last], 10, 64)
} // }}}

// func parsePlistDict {{{

func parsePlistDict(dict []byte, values map[string]string) error {
	d := xml.NewDecoder(bytes.NewReader(dict))
	var key string
	var tok xml.Token
	var err error

	for {
		tok, err = d.Token()
		if err == io.EOF || tok == nil {
			break
		}
		if err != nil {
			return err
		}

		switch tt := tok.(type) {
		case xml.StartElement:
			switch tt.Name.Local {
			case "key":
				if err = d.DecodeElement(&key, &tt); err != nil {
					return err
				}
				// Make sure this is something we care. If not, no need to proceed.
				switch key {
				case "machine_name", "machine_model", "cpu_type", "current_processor_speed", "number_processors", "os_version", "kernel_version":
				default:
					key = ""
				}
			case "string":
				// If key is not set, nothing to do.
				if key == "" {
					continue
				}
				var v string
				if err = d.DecodeElement(&v, &tt); err != nil {
					return err
				}
				values[key] = v
				key = ""
			case "integer":
				if key == "" {
					continue
				}
				var v int64
				if err = d.DecodeElement(&v, &tt); err != nil {
					return err
				}
				values[key] = strconv.FormatInt(v, 10)
				key = ""
			case "real":
				if key == "" {
					continue
				}
				var v float64
				if err = d.DecodeElement(&v, &tt); err != nil {
					return err
				}
				values[key] = fmt.Sprintf("%g", v)
				key = ""
			default:
			}
		}
	}

	return nil
} // }}}
