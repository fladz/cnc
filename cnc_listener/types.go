package main

// Prefix used for generating result file.
//
// Result file name will be {filenamePrefix}_{unix_timestamp}
const filenamePrefix = "cnc_result"

type BigQueryJSON struct {
	Start string `bigquery:"start"`
	// Recommended to use string for (u)int64 values.
	StartUnix string           `bigquery:"start_unix"`
	Duration  string           `bigquery:"duration,nullable"`
	IP        IPBQJSON         `bigquery:"ip"`
	OS        OSBQJSON         `bigquery:"os"`
	CPU       CPUBQJSON        `bigquery:"cpu"`
	Mem       MemBQJSON        `bigquery:"memory"`
	Ping      []ExecBQJSON     `bigquery:"ping"`
	Trace     []ExecBQJSON     `bigquery:"trace"`
	Download  []DownloadBQJSON `bigquery:"download"`
}

type IPBQJSON struct {
	IP      string `bigquery:"ip,nullable"`
	ISP     string `bigquery:"isp,nullable"`
	Country string `bigquery:"country,nullable"`
	City    string `bigquery:"city,nullable"`
}

type OSBQJSON struct {
	Name    string `bigquery:"name,nullable"`
	Version string `bigquery:"version,nullable"`
	Kernel  string `bigquery:"kernel,nullable"`
	Model   string `bigquery:"model,nullable"`
	Build   string `bigquery:"build,nullable"`
}

type CPUBQJSON struct {
	Number int    `bigquery:"number,nullable"`
	Speed  string `bigquery:"speed,nullable"` // Mac only
	CPU    string `bigquery:"cpu"`
}

type MemBQJSON struct {
	Physical MemBQ `bigquery:"physical"`
	Virtual  MemBQ `bigquery:virtual"`
}

type MemBQ struct {
	// Recommended to use string for (u)int64 values
	//	Total string `bigquery:"total,nullable"`
	//	Free  string `bigquery:"free,nullable"`
	//	Used  string `biguqery:"used,nullable"`
	Total uint64 `bigquery:"free,nullable"`
	Free  uint64 `bigquery:"free,nullable"`
	Used  uint64 `bigquery:"used,nullable"`
}

type ExecBQJSON struct {
	Location string `bigquery:"location,nullable"`
	Output   string `bigquery:"output,nullable"`
}

type DownloadBQJSON struct {
	Location string `bigquery:"location,nullable"`
	File     string `bigquery:"file,nullable"`
	Speed    string `bigquery:"speed,nullable"`
}

// Output structure.
type OutputJSON struct {
	Start     string         `json:"start"`
	StartUnix int64          `json:"start_unix"`
	Duration  string         `json:"duration"`
	IP        IPJSON         `json:"ip"`
	OS        OSJSON         `json:"os"`
	CPU       CPUJSON        `json:"cpu"`
	Mem       MemJSON        `json:"memory"`
	Ping      []ExecJSON     `json:"ping"`
	Trace     []ExecJSON     `json:"trace"`
	Download  []DownloadJSON `json:"download"`
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
	Build   string `json:"build"`
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
	Location     string   `json:"location"`
	Output       []string `json:"output"`
	OutputString string
}

type DownloadJSON struct {
	Location string `json:"location"`
	File     string `json:"file"`
	Speed    string `json:"speed"`
}
