package main

import (
	"bytes"
	"cloud.google.com/go/bigquery"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/appengine"
	"google.golang.org/appengine/log"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"
	"text/template"
	"time"
)

var (
	jwt, parentId, doc_template       string
	is_team_drive                     bool
	dataset_id, table_id, schema_file string
)

// func init {{{

func init() {
	// Parse env vars.
	jwt = os.Getenv("jwt")
	parentId = os.Getenv("parent_id")
	doc_template = os.Getenv("doc_template")
	dataset_id = os.Getenv("dataset_id")
	table_id = os.Getenv("table_id")
	schema_file = os.Getenv("schema_file")

	tmp := os.Getenv("is_team_drive")
	if strings.ToLower(tmp) == "true" {
		is_team_drive = true
	}

	http.HandleFunc("/", LogHandler)
} // }}}

// func LogHandler {{{
//
// Handles result upload, generate a Google doc and upload it to
// a specified Google Drive.
func LogHandler(w http.ResponseWriter, r *http.Request) {
	// Create a request context.
	ctx := appengine.NewContext(r)
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, time.Minute)
	defer cancel()

	// Sanity.
	if jwt == "" || parentId == "" {
		log.Criticalf(ctx, "not enough configuration, cannot process!")
		return
	}

	// The request should be in a JSON format.
	if r.Body == nil {
		log.Infof(ctx, "empty body, nothing to process")
		return
	}

	var contents OutputJSON
	var err error
	if err = json.NewDecoder(r.Body).Decode(&contents); err != nil {
		log.Warningf(ctx, "cannot unmarshal json data - %s", err)
		return
	}
	r.Body.Close()

	// Upload the data in Google Drive.
	if err = upload(ctx, contents); err != nil {
		log.Warningf(ctx, "(upload) %s", err)
		// Even if uploading a doc fails, we still want to insert the result in bigquery,
		// so not exiting here yet.
		// TODO: add retry
	} else {
		log.Infof(ctx, "upload complete")
	}

	// Save the data in BigQuery.
	if err := insert(ctx, contents); err != nil {
		log.Warningf(ctx, "(insert) %s", err)
		/// TODO: add retry
		return
	}
	log.Infof(ctx, "insert complete")
} // }}}

// func initDriveService {{{

func initDriveService(ctx context.Context) (*drive.Service, error) {
	jkey, err := ioutil.ReadFile(jwt)
	if err != nil {
		return nil, fmt.Errorf("error reading jwt - %s", err)
	}
	config, err := google.JWTConfigFromJSON(jkey, drive.DriveScope)
	if err != nil {
		return nil, fmt.Errorf("error creating jwt config - %s", err)
	}
	client := config.Client(ctx)

	return drive.New(client)
} // }}}

// func insert {{{

// Insert the data into our BigQuery.
func insert(ctx context.Context, data OutputJSON) error {
	// Get project id where this GAE instance runs on.
	projectId := os.Getenv("project_id")
	if projectId == "" {
		projectId = appengine.AppID(ctx)
		os.Setenv("project_id", projectId)
	}
	log.Infof(ctx, "inserting data...")
	log.Infof(ctx, "project_id - %q", projectId)

	// Read in schema.
	var schema bigquery.Schema
	f, err := os.Open(schema_file)
	if err != nil {
		return fmt.Errorf("error reading schema: %s", err)
	}
	if err = json.NewDecoder(f).Decode(&schema); err != nil {
		return fmt.Errorf("error decoding schema: %s", err)
	}
	f.Close()

	// Init bigquery service.
	bq, err := bigquery.NewClient(ctx, projectId)
	if err != nil {
		return err
	}

	// Transform data into bigquery struct.
	bqjson := convertJSONtoBigQueryJSON(data)
	if bqjson.Start == "" {
		return errors.New("error converting JSON into BigQuery data")
	}

	// Get table and its inserter.
	ins := bq.Dataset(dataset_id).Table(table_id).Inserter()
	// Set options.
	ins.SkipInvalidRows = true
	ins.IgnoreUnknownValues = true
	item := &bigquery.StructSaver{Schema: schema, Struct: bqjson}

	// Insert the data.
	if err = ins.Put(ctx, item); err != nil {
		log.Warningf(ctx, "error inserting data to BQ - %s", err)
		return err
	}

	log.Infof(ctx, "inserted data")
	return nil
} // }}}

// func upload {{{

// Create a doc using received struct data, then upload to a configured drive.
func upload(ctx context.Context, data OutputJSON) error {
	// Init Google Drive service.
	srv, err := initDriveService(ctx)
	if err != nil {
		return err
	}

	log.Infof(ctx, "uploading file...")

	// Sanity.
	if doc_template == "" {
		return errors.New("doc_template not set, cannot generate a file")
	}

	// Transform JSON data into a "easy-to-read" formatted text.
	t, err := template.ParseFiles(doc_template)
	if err != nil {
		return err
	}

	// Save the template data into bytes, then use that to
	// generate a doc.
	b := new(bytes.Buffer)
	if err = t.Execute(b, data); err != nil {
		return err
	}

	// Send the metadata and content.
	newFile := &drive.File{
		Name:     "cnc_result_" + strconv.Itoa(int(data.StartUnix)),
		Parents:  []string{parentId},
		MimeType: "application/vnd.google-apps.document",
	}

	// And set the source file's content-type ("text/csv") in Media() call.
	fcc := srv.Files.Create(newFile).Media(b, googleapi.ContentType("text/plain"))
	// Is it for team drive? If so, set a specific option.
	if is_team_drive {
		fcc.SupportsTeamDrives(true)
	}
	if _, err := fcc.Do(); err != nil {
		return fmt.Errorf("error uploading a doc - %s", err)
	}

	return nil
} // }}}

// func convertJSONtoBigQueryJSON {{{

func convertJSONtoBigQueryJSON(data OutputJSON) BigQueryJSON {
	res := BigQueryJSON{
		Start:     data.Start,
		StartUnix: strconv.FormatInt(data.StartUnix, 10),
		Duration:  data.Duration,
		IP: IPBQJSON{
			IP:      data.IP.IP,
			ISP:     data.IP.ISP,
			Country: data.IP.Country,
			City:    data.IP.City,
		},
		OS: OSBQJSON{
			Name:    data.OS.Name,
			Version: data.OS.Version,
			Kernel:  data.OS.Kernel,
			Model:   data.OS.Model,
			Build:   data.OS.Build,
		},
		CPU: CPUBQJSON{
			Number: data.CPU.Number,
			Speed:  data.CPU.Speed,
			CPU:    strings.Join(data.CPU.CPU, "\n"),
		},
		Mem: MemBQJSON{
			Physical: MemBQ{
				Total: data.Mem.Physical.Total,
				Free:  data.Mem.Physical.Free,
				Used:  data.Mem.Physical.Used,
			},
			Virtual: MemBQ{
				Total: data.Mem.Virtual.Total,
				Free:  data.Mem.Virtual.Free,
				Used:  data.Mem.Virtual.Used,
			},
		},
	}

	res.Ping = make([]ExecBQJSON, len(data.Ping))
	for i, _ := range data.Ping {
		res.Ping[i] = ExecBQJSON{Location: data.Ping[i].Location, Output: strings.Join(data.Ping[i].Output, "\n")}
	}
	res.Trace = make([]ExecBQJSON, len(data.Trace))
	for i, _ := range data.Trace {
		res.Trace[i] = ExecBQJSON{Location: data.Trace[i].Location, Output: strings.Join(data.Trace[i].Output, "\n")}
	}
	res.Download = make([]DownloadBQJSON, len(data.Download))
	for i, _ := range data.Download {
		res.Download[i] = DownloadBQJSON{Location: data.Download[i].Location, File: data.Download[i].File, Speed: data.Download[i].Speed}
	}
	return res
}
