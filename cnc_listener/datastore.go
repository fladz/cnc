package main

import (
	"context"
	"google.golang.org/appengine/datastore"
	"google.golang.org/appengine/log"
	"reflect"
)

// Datastore kind for storing list of existing subfolders.
const dsKindFolder = "subfolder"

// func getFilesFromDatastore {{{

func getFilesFromDatastore(ctx context.Context) (googleDriveSubfolders, error) {
	var list = make(googleDriveSubfolders, 0)
	var raw []googleDriveFileInfo
	query := datastore.NewQuery(dsKindFolder)

	if _, err := query.GetAll(ctx, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}

	// Sanity
	for _, data := range raw {
		if data.Name == "" || data.Id == "" {
			continue
		}
		list[data.Name] = data.Id
	}

	return list, nil
} // }}}

// func getFileFromDatastore {{{

func getFileFromDatastore(ctx context.Context, key string) (googleDriveFileInfo, error) {
	query := datastore.NewQuery(dsKindFolder).Filter("Name =", key).Limit(1)
	var row googleDriveFileInfo

	i := query.Run(ctx)
	var err error
	_, err = i.Next(&row)
	for err == nil {
		_, err = i.Next(&row)
	}
	if err != datastore.Done {
		return row, err
	}

	return row, nil
} // }}}

// func casSubfolderInDatastore {{{

func casSubfolderInDatastore(ctx context.Context, curmap, newmap googleDriveSubfolders) {
	if reflect.DeepEqual(curmap, newmap) {
		// Same data, nothing to do.
		log.Infof(ctx, "no subfolder change found, not updating state")
		return
	}

	log.Infof(ctx, "subfolder values changed, checking diffs...")

	var name, curid, newid string
	var inserted, updated, removed, failed int
	var err error

	// Check if any is added or updated.
	for name, newid = range newmap {
		curid = curmap[name]
		switch {
		case curid == "":
			log.Infof(ctx, "new folderId found (name=%s, id=%s)", name, newid)
			if _, err = datastore.Put(ctx, datastore.NewKey(ctx, dsKindFolder, newid, 0, nil), &googleDriveFileInfo{Name: name, Id: newid}); err != nil {
				log.Infof(ctx, "error inserting folder data (name=%s, id=%s) %s", name, newid, err)
				failed++
			} else {
				log.Infof(ctx, "inserted folder data (name=%s, id=%s)", name, newid)
				inserted++
			}
		case curid != newid:
			log.Infof(ctx, "folderId changed %s -> %s (%s)", curid, newid, name)
			// Upsert new id.
			if _, err = datastore.Put(ctx, datastore.NewKey(ctx, dsKindFolder, newid, 0, nil), &googleDriveFileInfo{Name: name, Id: newid}); err != nil {
				log.Warningf(ctx, "error upserting folder data (name=%s, id=%s) %s", name, newid, err)
				failed++
			} else {
				log.Infof(ctx, "upserted folder data (name=%s, id=%s)", name, newid)
				updated++
			}
			// Remove old id.
			if err = datastore.Delete(ctx, datastore.NewKey(ctx, dsKindFolder, curid, 0, nil)); err != nil {
				log.Warningf(ctx, "error removing folder data (name=%s, id=%s) %s", name, curid, err)
				failed++
			} else {
				log.Infof(ctx, "removed folder data (name=%s, id=%s)", name, curid)
				removed++
			}
		default:
			// No change.
		}
		// Value checked, delete this data from current map.
		delete(curmap, name)
	}

	// Any remaining in the current map means they're deleted.
	for name, curid = range curmap {
		log.Infof(ctx, "subfolder %s deleted (%s)", name, curid)
		if err = datastore.Delete(ctx, datastore.NewKey(ctx, dsKindFolder, curid, 0, nil)); err != nil {
			log.Warningf(ctx, "error removing folder data (name=%s, id=%s) %s", name, curid, err)
			failed++
		} else {
			log.Infof(ctx, "removed folder data (name=%s, id=%s)", name, curid)
			removed++
		}
	}

	log.Infof(ctx, "finished updating subfolder state: inserted=%d, updated=%d, removed=%d, failed=%d",
		inserted, updated, removed, failed)
} // }}}
