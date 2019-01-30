package main

import (
	"bytes"
	"cloud.google.com/go/bigquery"
	"cloud.google.com/go/storage"
	"context"
	"encoding/gob"
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
	jwt, parentId, doc_template                   string
	is_team_drive, enable_drive_subfolders, debug bool
	dataset_id, table_id, schema_file             string
	retry_bucket_uploads, retry_bucket_inserts    string
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
	retry_bucket_uploads = os.Getenv("retry_bucket_uploads")
	retry_bucket_inserts = os.Getenv("retry_bucket_inserts")

	if strings.ToLower(os.Getenv("is_team_drive")) == "true" {
		is_team_drive = true
	}
	if strings.ToLower(os.Getenv("enable_drive_subfolders")) == "true" {
		enable_drive_subfolders = true
	}
	if strings.ToLower(os.Getenv("debug")) == "true" {
		debug = true
	}

	http.HandleFunc("/tasks/subfolder/", CronFolderHandler)
	http.HandleFunc("/tasks/retry/", CronRetryHandler)
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

	// Another sanity.
	// The request should only be at "/". If the request URI contains any more than
	// that, it's an invalid request.
	if r.RequestURI != "/" {
		log.Infof(ctx, "reject invalid request (uri=%q)", r.RequestURI)
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
		//
		// Save the failed entry in retry state.
		if err = saveRetryState(ctx, retry_bucket_uploads, contents); err != nil {
			// Failed saving retry state? :/
			log.Warningf(ctx, "(upload) error saving retry state (%d) %s", contents.StartUnix, err)
		} else {
			log.Infof(ctx, "(upload) saved retry state (%d)", contents.StartUnix)
		}
	} else {
		log.Infof(ctx, "upload complete")
	}

	// Save the data in BigQuery.
	if err := insert(ctx, contents); err != nil {
		log.Warningf(ctx, "(insert) %s", err)
		// Failed inserting, save retry state.
		if err = saveRetryState(ctx, retry_bucket_inserts, contents); err != nil {
			// Failed saving retry state? :/
			log.Warningf(ctx, "(insert) error saving retry state (%d) %s", contents.StartUnix, err)
		} else {
			log.Infof(ctx, "(insert) saved retry state (%d)", contents.StartUnix)
		}
		return
	}
	log.Infof(ctx, "insert complete")
} // }}}

// func initDriveService {{{

func initDriveService(ctx context.Context) (*drive.Service, error) {
	if jwt == "" {
		return nil, errors.New("missing jwt in configuration")
	}

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

// func saveRetryState {{{

func saveRetryState(ctx context.Context, bucket string, data OutputJSON) error {
	// Sanity.
	if bucket == "" {
		return errors.New("missing bucket configuration")
	}

	// Prep storage client.
	client, err := storage.NewClient(ctx)
	if err != nil {
		return fmt.Errorf("error initializing storage client: %s", err)
	}

	// Convert the struct data into bytes.
	buf := new(bytes.Buffer)
	enc := gob.NewEncoder(buf)
	enc.Encode(data)

	// Create an object, use unix timestamp as a key.
	w := client.Bucket(bucket).Object(strconv.Itoa(int(data.StartUnix))).NewWriter(ctx)
	defer w.Close()
	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("error writing state: %s", err)
	}

	return nil
} // }}}

// func insert {{{

// Insert the data into our BigQuery.
func insert(ctx context.Context, data OutputJSON) error {
	// Sanity
	if dataset_id == "" || table_id == "" || schema_file == "" {
		return errors.New("missing BigQuery configuration")
	}

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
	// Sanity
	if parentId == "" || doc_template == "" {
		return errors.New("missing Google Drive configuration")
	}

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

	// What filename should it be?
	filename := filenamePrefix + "_" + strconv.Itoa(int(data.StartUnix))

	// If subfolder upload is enabled, check if a proper subfolder
	// already exists.
	var subfolderId = parentId
	if enable_drive_subfolders {
		subfolderName := extractSubfolderNameFromFileName(time.Now(), filename)
		if subfolderName == "" {
			log.Infof(ctx, "unable to determine subfolder name (%s)", filename)
		} else {
			log.Infof(ctx, "retrieving id for folder %s (file=%s)", subfolderName, filename)
			fi, err := getFileFromDatastore(ctx, subfolderName)
			if err != nil || fi.Name == "" || fi.Id == "" {
				// Folder not found, upload to the root folder.
				log.Infof(ctx, "unable to retrieve id for subfolder %s (name=%s, id=%s, err=%s)", subfolderName, fi.Name, fi.Id, err)
			} else {
				log.Infof(ctx, "retrieved id for subfolder %s (%s)", subfolderName, fi.Id)
				subfolderId = fi.Id
			}
		}
	}

	log.Infof(ctx, "uploading file (%s) in folder (%s)", filename, subfolderId)

	// Send the metadata and content.
	newFile := &drive.File{
		Name:     filename,
		Parents:  []string{subfolderId},
		MimeType: driveDocMimeType,
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
