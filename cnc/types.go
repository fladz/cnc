package main

const (
	// Endpoint for obtaining ISP data.
	// Set your favorite API endpoint here.
	ispEndpoint = "https://isp-api.com"

	// Endpoint for uploading test results.
	// Set your own instance of uploader here.
	uploadEndpoint = "https://upload-endpoint.com"

	// Max hops for trace
	hops = 30
)

type downloadTestSource struct {
	label, url string
}

var (
	// Endpoint for ping & trace
	netDestinations = []string{
		// Add your ping endpoints here.
		"1.2.3.4",
		"10.20.30.40",
	}

	// Test file paths for download speed check.
	downloadTestSources = []*downloadTestSource{
		// Add your files here.
		&downloadTestSource{
			label: "location1",
			url:   "https://location1.com/test.txt",
		},
	}

	// Test results.
	results OutputJSON
)

// Output structure.
type OutputJSON struct {
	// Date/Time this test started.
	Start     string `json:"start"`
	StartUnix int64  `json:"start_unix"`

	// Duration of test.
	Duration string `json:"duration"`

	// Each test results.
	IP       IPJSON         `json:"ip"`
	OS       OSJSON         `json:"os"`
	CPU      CPUJSON        `json:"cpu"`
	Mem      MemJSON        `json:"memory"`
	Ping     []ExecJSON     `json:"ping"`
	Trace    []ExecJSON     `json:"trace"`
	Download []DownloadJSON `json:"download"`
}

type IPJSON struct {
	IP      string `json:"ip"`
	ISP     string `json:"isp"`
	Country string `json:"countrycode"`
	City    string `json:"city"`
}

type OSJSON struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Kernel  string `json:"kernel"`
	Model   string `json:"model"`
	Build   string `json:"build"` // Windows only
}

type CPUJSON struct {
	Number int      `json:"number"`
	Speed  string   `json:"speed"` // Mac only
	CPU    []string `json:"cpu"`
}

type MemJSON struct {
	Physical Mem `json:"physical"`
	Virtual  Mem `json:"virtual"`
}

type Mem struct {
	Total uint64 `json:"total"`
	Free  uint64 `json:"free"`
	Used  uint64 `json:"used"`
}

type ExecJSON struct {
	Location string   `json:"location"`
	Output   []string `json:"output"`
}

type DownloadJSON struct {
	Location string `json:"location"`
	File     string `json:"file"`
	Speed    string `json:"speed"`
}
