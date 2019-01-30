package main

import (
	"bytes"
	"cloud.google.com/go/storage"
	"context"
	"encoding/gob"
	"fmt"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/iterator"
	"google.golang.org/appengine"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// List of files to be moved to a subfolder.
// Key is a name of subfolder, value is list of files to be moved.
type googleDriveFileMove map[string][]googleDriveFileInfo
type googleDriveFileInfo struct {
	Name string // Name of the file
	Id   string // Id of the file
}

// List of existing subfolders and their id.
// Key is folder name, value is id.
type googleDriveSubfolders map[string]string

// Detail of Google Drive files.
type googleDriveFileType struct {
	Name     string // Name of the file
	Id       string // Id of the file
	IsFolder bool   // If the file is a folder
	FName    string // If the file is a file, which folder this file should be in
}

const (
	// Format used to generate subfolder name from timestamp.
	timeFormat = "200601"
	// Mimetypes
	driveFolderMimeType = "application/vnd.google-apps.folder"
	driveDocMimeType    = "application/vnd.google-apps.document"
)

// func CronFolderHandler {{{

// Cronjob to
// (1) Create a G-drive subfolder
// (2) Move result file(s) to a proper subfolder if a result file is in G-Drive folder itself.
func CronFolderHandler(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, time.Minute)
	defer cancel()

	// The cron request should have this header value.
	// If it's not present, reject.
	if r.Header.Get("X-Appengine-Cron") == "" {
		log.Warningf(ctx, "reject invalid cron request")
		return
	}

	// Get list of subfolders we have in datastore.
	currentSubfolders, err := getFilesFromDatastore(ctx)
	if err != nil {
		log.Warningf(ctx, "error retrieving existing subfolders - %s", err)
		return
	}
	log.Infof(ctx, "retrieved %d subfolder info from state", len(currentSubfolders))

	// Debug logs.
	if debug {
		for name, id := range currentSubfolders {
			log.Infof(ctx, "DEBUG: subfolder retrieved from state: name=%s, id=%s", name, id)
		}
	}

	// Init drive service.
	srv, err := initDriveService(ctx)
	if err != nil {
		log.Warningf(ctx, "error initializing drive service - %s", err)
		return
	}

	// Get list of existing subfolders and result files.
	subfolders, files, err := getFilesFromGoogleDrive(ctx, srv)
	if err != nil {
		log.Warningf(ctx, "error retrieving existing files - %s", err)
		return
	}

	log.Infof(ctx, "retrieved %d folders and %d files", len(subfolders), len(files))

	// Debug logs.
	if debug {
		for name, id := range subfolders {
			log.Infof(ctx, "DEBUG: subfolder retrieved from Google Drive: name=%s, id=%s", name, id)
		}
		for sname, values := range files {
			log.Infof(ctx, "%d files need to be moved to subfolder %s", len(values), sname)
			for _, value := range values {
				log.Infof(ctx, "  file: name=%s, id=%s", value.Name, value.Id)
			}
		}
	}

	// Create a subfolder for 2 days from now.
	var subfolderId string
	subfolder := time.Now().Add(24 * 2 * time.Hour).Format(timeFormat)
	log.Infof(ctx, "checking subfolder existence (%s)", subfolder)
	// Do we already have this subfolder?
	if subfolderId = subfolders[subfolder]; subfolderId != "" {
		log.Infof(ctx, "subfolder %s already exists (id=%s), not creating", subfolder, subfolderId)
	} else {
		// Not exists, let's create.
		if subfolderId, err = createGoogleDriveSubfolder(ctx, srv, subfolder); err != nil {
			log.Warningf(ctx, "error creating a subfolder (%s) - %s", subfolder, err)
			return
		}
		// Ok subfolder created, let's save this in the subfolder map.
		subfolders[subfolder] = subfolderId
		log.Infof(ctx, "subfolder %s created (id=%s)", subfolder, subfolderId)
	}

	// If any folder values are changed, update datastore.
	casSubfolderInDatastore(ctx, currentSubfolders, subfolders)

	// Move any result files.
	if len(files) == 0 {
		log.Infof(ctx, "no file to move")
		return
	}

	var subfolderCreated, subfolderCreateFailed, fileMoved, fileMoveFailed int
	for subfolderName, values := range files {
		log.Infof(ctx, "checking subfolder id for %d files (name=%s)", len(values), subfolderName)

		// Do we have this folder?
		if subfolderId = subfolders[subfolderName]; subfolderId == "" {
			// Nope, let's create first.
			log.Infof(ctx, "subfolder %s not exists, need to create", subfolderName)
			subfolderId, err = createGoogleDriveSubfolder(ctx, srv, subfolderName)
			if err != nil || subfolderId == "" {
				log.Warningf(ctx, "error creating subfolder (%s) %s", subfolderName, err)
				subfolderCreateFailed++
				fileMoveFailed = fileMoveFailed + len(values)
				continue
			}

			log.Infof(ctx, "subfolder %s created (id=%s)", subfolderName, subfolderId)
			subfolderCreated++

			// Update state.
			if _, err = datastore.Put(ctx, datastore.NewKey(ctx, dsKindFolder, subfolderId, 0, nil), &googleDriveFileInfo{Name: subfolderName, Id: subfolderId}); err != nil {
				log.Warningf(ctx, "error inserting folder data (name=%s, id=%s) %s", subfolderName, subfolderId, err)
			} else {
				log.Infof(ctx, "inserted folder data (name=%s, id=%s)", subfolderName, subfolderId)
			}
		}

		log.Infof(ctx, "moving %d files to subfolder (name=%s, id=%s)", len(values), subfolderName, subfolderId)

		for _, file := range values {
			if err = moveGoogleDriveFile(ctx, srv, subfolderId, file); err != nil {
				log.Warningf(ctx, "error moving file %s (%s) to folder %s (%s) %s",
					file.Name, file.Id, subfolderName, subfolderId, err)
				fileMoveFailed++
				continue
			}

			log.Infof(ctx, "moved file %s (%s) to folder %s (%s)", file.Name, file.Id, subfolderName, subfolderId)
			fileMoved++
		}
	}

	log.Infof(ctx, "Finished cronjob (subfolder_created=%d, subfolder_create_failed=%d, files_moved=%d, file_move_failed=%d)",
		subfolderCreated, subfolderCreateFailed, fileMoved, fileMoveFailed)
} // }}}

// func CronRetryHandler {{{

// Cronjob to retry failed Google Drive uploads and BigQuery inserts if any.
func CronRetryHandler(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, time.Minute)
	defer cancel()

	// The cron request should have this header value.
	// If it's not present, reject.
	if r.Header.Get("X-Appengine-Cron") == "" {
		log.Warningf(ctx, "reject invalid cron request")
		return
	}

	// Open up cloud storage client.
	client, err := storage.NewClient(ctx)
	if err != nil {
		log.Warningf(ctx, "error initializing storage client - %s", err)
		return
	}

	// Process BQ retries.
	log.Infof(ctx, "start processing BQ retries")
	if err = processRetries(ctx, client.Bucket(retry_bucket_inserts), insert); err != nil {
		log.Warningf(ctx, "(insert) %s", err)
	}

	// Process Google Drive retries.
	log.Infof(ctx, "start processing Drive retries")
	if err = processRetries(ctx, client.Bucket(retry_bucket_uploads), upload); err != nil {
		log.Warningf(ctx, "(upload) %s", err)
	}
} // }}}

// func processRetries {{{

func processRetries(ctx context.Context, bucket *storage.BucketHandle, fn func(context.Context, OutputJSON) error) error {
	// Get list of retry keys.
	var keys []string
	it := bucket.Objects(ctx, nil)
	for {
		attr, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}
		// Save the key in key slice.
		keys = append(keys, attr.Name)
	}
	log.Infof(ctx, "retrieved %d state keys", len(keys))

	if len(keys) == 0 {
		// No retry to do.
		log.Infof(ctx, "no retry to perform")
		return nil
	}

	// Now get the actual values.
	var errRetrieve, doneRetry, errRetry, doneStateRemove, errStateRemove int
	for _, key := range keys {
		if debug {
			log.Infof(ctx, "DEBUG: retrieving retry value (key=%s)", key)
		}
		obj := bucket.Object(key)
		r, err := obj.NewReader(ctx)
		if err != nil {
			if err == storage.ErrObjectNotExist {
				// Object no longer exists
				continue
			}
			log.Warningf(ctx, "error retrieving retry value (key=%s)", key)
			errRetrieve++
			continue
		}

		// Decode into our struct.
		b, err := ioutil.ReadAll(r)
		if err != nil {
			log.Warningf(ctx, "error reading retry value (key=%s) %s", key, err)
			errRetrieve++
			continue
		}
		var data OutputJSON
		var buf = bytes.NewBuffer(b)
		if err = gob.NewDecoder(buf).Decode(&data); err != nil {
			log.Warningf(ctx, "error decoding retry value (key=%s) %s", key, err)
			errRetrieve++
			continue
		}

		// Retry the failed operation.
		if err = fn(ctx, data); err != nil {
			// Failed again.
			log.Warningf(ctx, "error retrying operation (key=%s) %s", key, err)
			errRetry++
			continue
		}

		log.Infof(ctx, "finished retry operation (key=%s)", key)
		doneRetry++
		// Operation successfully done this time, remove the state.
		if err = obj.Delete(ctx); err != nil {
			if err == storage.ErrObjectNotExist {
				continue
			}
			log.Warningf(ctx, "error removing state (key=%s) %s", key, err)
			errStateRemove++
		} else {
			// Removed state.
			log.Infof(ctx, "removed retry state (key=%s)", key)
			doneStateRemove++
		}
	}

	log.Infof(ctx, "finished processing (retrieve_error=%d, retry_success=%d, retry_fail=%d, state_remove=%d, state_remove_error=%d",
		errRetrieve, doneRetry, errRetry, doneStateRemove, errStateRemove)

	return nil
} // }}}

// func createGoogleDriveSubfolder {{{

// Check if requested subfolder exists in G-Drive and create if not.
// Return id of the folder and error if any.
func createGoogleDriveSubfolder(ctx context.Context, srv *drive.Service, name string) (string, error) {
	log.Infof(ctx, "creating subfolder %s", name)
	folder := &drive.File{
		Name:     name,
		Parents:  []string{parentId},
		MimeType: driveFolderMimeType,
	}
	created, err := srv.Files.Create(folder).SupportsTeamDrives(is_team_drive).Do()
	if err != nil {
		return "", err
	}

	return created.Id, nil
} // }}}

// func moveGoogleDriveFile {{{

// Move a requested file to a specified destination.
func moveGoogleDriveFile(ctx context.Context, srv *drive.Service, dst string, file googleDriveFileInfo) error {
	_, err := srv.Files.Update(file.Id, nil).SupportsTeamDrives(is_team_drive).RemoveParents(parentId).AddParents(dst).Do()
	if err != nil {
		return err
	}

	return nil
} // }}}

// func extractSubfolderNameFromFileName {{{

// Return YYYYMM formatted subfolder name from the given cnc_result_{epoch} filename.
func extractSubfolderNameFromFileName(now time.Time, filename string) string {
	// Sanity
	if !strings.HasPrefix(filename, "cnc_result_") {
		return ""
	}

	tmp, err := strconv.ParseInt(filename[len("cnc_result_"):], 10, 64)
	if err != nil {
		return ""
	}

	// Let's convert this epoch into time.Time.
	t := time.Unix(tmp, 0)
	if t.IsZero() || t.After(now) {
		// If the number extracted from the filename is invalid epoch (zero)
		// or future, reject it.
		return ""
	}

	return t.Format(timeFormat)
} // }}}

// func getFilesFromGoogleDrive {{{

// Check all existing files in GoogleDrive and return
// 1) list of subfolders - name and id for each existing subfolder
// 2) list of result files that are not in a subfolder yet and need to be moved to a subfolder

func getFilesFromGoogleDrive(ctx context.Context, srv *drive.Service) (googleDriveSubfolders, googleDriveFileMove, error) {
	var nextToken string
	var err error
	var folders = make(googleDriveSubfolders, 0)
	var files = make(googleDriveFileMove, 0)
	var tmpFiles []googleDriveFileType

	for {
		if tmpFiles, nextToken, err = googleDriveFileList(ctx, srv, nextToken); err != nil {
			return nil, nil, err
		}

		// Save returned value in the final maps.
		for _, tf := range tmpFiles {
			switch tf.IsFolder {
			case true:
				folders[tf.Name] = tf.Id
			default:
				// Sanity
				if tf.FName == "" {
					continue
				}
				if files[tf.FName] == nil {
					files[tf.FName] = make([]googleDriveFileInfo, 0)
				}
				files[tf.FName] = append(files[tf.FName], googleDriveFileInfo{Name: tf.Name, Id: tf.Id})
			}
		}

		// If no more page to process, nothing to do.
		if nextToken == "" {
			break
		}
	}

	return folders, files, nil
} // }}}

// func googleDriveFileList {{{

// List up files in a specified Google Drive and return.
// The files supported here are folders and doc files.

func googleDriveFileList(ctx context.Context, srv *drive.Service, token string) ([]googleDriveFileType, string, error) {
	fl := srv.Files.List().Context(ctx).OrderBy("name desc").Fields("nextPageToken, files(id,name,mimeType)")
	if token != "" {
		fl.PageToken(token)
	}
	if is_team_drive {
		fl.SupportsTeamDrives(true).IncludeTeamDriveItems(true)
	}
	fl.Q(fmt.Sprintf("'%s' in parents", parentId)).PageSize(10)
	fileList, err := fl.Do()
	if err != nil {
		return nil, "", err
	}

	// Sanity
	if len(fileList.Files) == 0 {
		return nil, fileList.NextPageToken, nil
	}

	// Loop through and list up files.
	var files = make([]googleDriveFileType, 0)
	now := time.Now()
	for _, file := range fileList.Files {
		if file.Trashed {
			continue
		}

		ft := googleDriveFileType{Name: file.Name, Id: file.Id}

		switch file.MimeType {
		case driveFolderMimeType:
			// This is a subfolder.
			ft.IsFolder = true
		case driveDocMimeType:
			// This is a file, let's check which folder it should be in.
			if ft.FName = extractSubfolderNameFromFileName(now, ft.Name); ft.FName == "" {
				// Invalid filename, skip
				continue
			}
		default:
			continue
		}

		// Save in the slice.
		files = append(files, ft)
	}

	return files, fileList.NextPageToken, nil
} // }}}
