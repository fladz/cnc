package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

var output, dir string

func main() {
	start := time.Now()

	// Signal handling.
	go listenSignal()

	// Setup output file path. Output will be written under current working
	// directory, with a file name of "{app_name}_{timestamp}.txt"
	var err error
	if dir, err = os.Getwd(); err == nil {
		output = dir + "/" + "cnc_result_" + strconv.Itoa(int(time.Now().Unix())) + ".txt"
		f, err := os.Create(output)
		if err != nil {
			fmt.Println("Error preparing output file, result will be written to stdout")
		} else {
			// Ok prepared output file, let's set the logger.
			log.SetOutput(f)
			fmt.Printf("Test result will be written in %s\n", output)
		}
	}

	// Remove default prefix of timestamps.
	log.SetFlags(log.Flags() &^ (log.Ldate | log.Ltime))

	// Init data struct for this test.
	results = OutputJSON{}

	// Start various tests.
	results.Start = start.Format("Mon Jan 2 2006 15:04:05 MST")
	results.StartUnix = start.Unix()
	log.Printf("Test started at: %s\n", results.Start)
	checkStart()

	log.Println("\n\nAll tasks have been completed")
	results.Duration = time.Since(start).String()
	log.Printf("Elapsed Time: %s\n", results.Duration)

	// Send the result.
	sendResults()

	if output != "" {
		fmt.Printf("\n\nTest completed, result is written in %s\n\n", output)
	} else {
		fmt.Println("\n\nTest completed\n\n")
	}
	fmt.Println("You could quit now or wait until this screen closes in 5 minutes")
	time.Sleep(5 * time.Minute)
}

func sendResults() {
	client := http.Client{}
	b := new(bytes.Buffer)
	json.NewEncoder(b).Encode(results)

	res, err := client.Post(uploadEndpoint, "application/json", b)
	if err != nil {
		log.Printf("error uploading results: %s", err)
		return
	}

	// Don't need the response.
	io.Copy(ioutil.Discard, res.Body)
	res.Body.Close()
}
