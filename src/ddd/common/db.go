package common

import (
	"database/sql"
	"fmt"

	log "github.com/sirupsen/logrus"
	// Required import as sql driver
	// not actually used by anything explicitly
	_ "github.com/mattn/go-sqlite3"
)

const (
	VolumeTable    = "volumes"
	NameKey        = "name"
	FsKey          = "filesystem"
	PersistenceKey = "persistence_mode"
	ConnKey        = "connections"
	UpdateKey      = "updated_at"
	DatabaseFile   = ".clientdb"
)

// Shared DB connection
var _dbcon *sql.DB

type VolObj struct {
	Name        string
	Filesystem  string
	Connections int
	Persistence string
	UpdatedAt   string
}

func PrepareDB() {
	// Open DB file
	log.Debugf("Opening database file: %s", DatabaseFile)
	database, err := sql.Open("sqlite3", DatabaseFile)
	PanicErr(err)
	// Check connection
	log.Debug("Checking database connection")
	err = database.Ping()
	PanicErr(err)
	// Create initial table if it hasn't been created
	log.Debugf("Creating initial table '%s' if it doesn't exist", VolumeTable)
	stmt := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s TEXT PRIMARY KEY, %s TEXT, %s INTEGER, %s TEXT, %s DATETIME DEFAULT CURRENT_TIMESTAMP);",
		VolumeTable, NameKey, FsKey, ConnKey, PersistenceKey, UpdateKey)
	statement, err := database.Prepare(stmt)
	PanicErr(err)
	_, err = statement.Exec()
	PanicErr(err)
	// Initializing global db connection variable.  The spec for sql.DB suggests
	// sharing this connection/keeping it open since it's designed to be long lived
	_dbcon = database
}

func getVolObj(name string) *VolObj {
	log.Debugf("Checking for existing volume object: %s", name)
	v := &VolObj{}
	stmt := fmt.Sprintf("SELECT * FROM %s WHERE %s = ?;", VolumeTable, NameKey)
	row := _dbcon.QueryRow(stmt, name)
	if err := row.Scan(&v.Name, &v.Filesystem, &v.Connections, &v.Persistence, &v.UpdatedAt); err != nil {
		if err != sql.ErrNoRows {
			PanicErr(err)
		}
	}

	return v
}

func UpsertVolObj(name, filesystem string, connections int, persistence string) *VolObj {
	// See if we already have a db record
	tmpv := &VolObj{name, filesystem, connections, persistence, ""}
	v := getVolObj(name)
	if v.Name == "" {
		log.Debugf("Creating new volume object: %s", name)
		// If we didn't have a record, we'll create a new one
		stmt := fmt.Sprintf("INSERT INTO %s (%s, %s, %s, %s) VALUES (?1, ?2, ?3, ?4);",
			VolumeTable, NameKey, FsKey, ConnKey, PersistenceKey)
		statement, err := _dbcon.Prepare(stmt)
		PanicErr(err)
		_, err = statement.Exec(name, filesystem, connections, persistence)
		PanicErr(err)
		v = tmpv
	} else if v != tmpv {
		log.Debugf("Volume object attributes do not match, updating database: %s", name)
		// Something didn't match, update the whole object with provided values
		stmt := fmt.Sprintf("UPDATE %s SET %s = ?1, %s = ?2, %s = ?3 WHERE %s = ?4",
			VolumeTable, FsKey, ConnKey, PersistenceKey, NameKey)
		statement, err := _dbcon.Prepare(stmt)
		PanicErr(err)
		_, err = statement.Exec(filesystem, connections, persistence, name)
		PanicErr(err)
		v = tmpv
	}
	return v
}

func (v *VolObj) AddConnection() error {
	log.Debugf("Incrementing connections for volume %s", v.Name)
	stmt := fmt.Sprintf("UPDATE %s SET %s = ?, %s = datetime('now', 'utc') WHERE %s = ?", VolumeTable, ConnKey, UpdateKey, NameKey)
	statement, err := _dbcon.Prepare(stmt)
	PanicErr(err)
	v.Connections++
	_, err = statement.Exec(v.Connections, v.Name)
	PanicErr(err)
	return nil
}

func (v *VolObj) DelConnection() error {
	log.Debugf("Decrementing connections for volume %s", v.Name)
	if v.Persistence == "manual" {
		if v.Connections > 0 {
			v.Connections--
		} else {
			log.Debugf("Connections for volume %s is already 0", v.Name)
			v.Connections = 0
		}
		stmt := fmt.Sprintf("UPDATE %s SET %s = ?, %s = datetime('now', 'utc') WHERE %s = ?", VolumeTable, ConnKey, UpdateKey, NameKey)
		statement, err := _dbcon.Prepare(stmt)
		PanicErr(err)
		_, err = statement.Exec(v.Connections, v.Name)
		PanicErr(err)
	} else {
		log.Debugf("Volume %s is non-persistent, deleting entry", v.Name)
		stmt := fmt.Sprintf("DELETE from %s WHERE %s = ?", VolumeTable, NameKey)
		statement, err := _dbcon.Prepare(stmt)
		PanicErr(err)
		_, err = statement.Exec(v.Name)
		PanicErr(err)
	}
	return nil
}

func (v *VolObj) ResetConnections() error {
	log.Debugf("Resetting connections for volume %s to 0", v.Name)
	stmt := fmt.Sprintf("UPDATE %s SET %s = ?, %s = datetime('now', 'utc') WHERE %s = ?", VolumeTable, ConnKey, UpdateKey, NameKey)
	statement, err := _dbcon.Prepare(stmt)
	PanicErr(err)
	_, err = statement.Exec(0, v.Name)
	PanicErr(err)
	return nil
}