package main

import (
	"database/sql"
	"fmt"

	log "github.com/sirupsen/logrus"
	// Required import as sql driver
	// not actually used by anything explicitly
	_ "github.com/mattn/go-sqlite3"
)

const (
	VolumeTable = "volumes"
	NameKey     = "name"
	FsKey       = "filesystem"
	ConnKey     = "connections"
	UpdateKey   = "updated_at"
)

// Shared DB connection
var _dbcon *sql.DB

type VolObj struct {
	Name        string
	Filesystem  string
	Connections int
	UpdatedAt   string
}

func prepareDB() {
	// Open DB file
	log.Debugf("Opening database file: %s", DatabaseFile)
	database, err := sql.Open("sqlite3", DatabaseFile)
	panicErr(err)
	// Check connection
	log.Debug("Checking database connection")
	err = database.Ping()
	panicErr(err)
	// Create initial table if it hasn't been created
	log.Debugf("Creating initial table '%s' if it doesn't exist", VolumeTable)
	stmt := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s TEXT PRIMARY KEY, %s TEXT, %s INTEGER, %s DATETIME DEFAULT CURRENT_TIMESTAMP);",
		VolumeTable, NameKey, FsKey, ConnKey, UpdateKey)
	statement, err := database.Prepare(stmt)
	panicErr(err)
	_, err = statement.Exec()
	panicErr(err)
	// Initializing global db connection variable.  The spec for sql.DB suggests
	// sharing this connection/keeping it open since it's designed to be long lived
	_dbcon = database
}

func getVolObj(name string) *VolObj {
	log.Debugf("Checking for existing volume object: %s", name)
	v := &VolObj{}
	stmt := fmt.Sprintf("SELECT * FROM %s WHERE %s = ?;", VolumeTable, NameKey)
	row := _dbcon.QueryRow(stmt, name)
	if err := row.Scan(&v.Name, &v.Filesystem, &v.Connections, &v.UpdatedAt); err != nil {
		if err != sql.ErrNoRows {
			panicErr(err)
		}
	}

	return v
}

func UpsertVolObj(name, filesystem string, connections int) *VolObj {
	// See if we already have a db record
	tmpv := &VolObj{name, filesystem, connections, ""}
	v := getVolObj(name)
	if v.Name == "" {
		log.Debugf("Creating new volume object: %s", name)
		// If we didn't have a record, we'll create a new one
		stmt := fmt.Sprintf("INSERT INTO %s (%s, %s, %s) VALUES (?1, ?2, ?3);",
			VolumeTable, NameKey, FsKey, ConnKey)
		statement, err := _dbcon.Prepare(stmt)
		panicErr(err)
		_, err = statement.Exec(name, filesystem, connections)
		panicErr(err)
		v = tmpv
	} else if v != tmpv {
		log.Debugf("Volume object attributes do not match, updating database: %s", name)
		// Something didn't match, update the whole object with provided values
		stmt := fmt.Sprintf("UPDATE %s SET %s = ?1, %s = ?2 WHERE %s = ?3",
			VolumeTable, FsKey, ConnKey, NameKey)
		statement, err := _dbcon.Prepare(stmt)
		panicErr(err)
		_, err = statement.Exec(filesystem, connections, name)
		panicErr(err)
		v = tmpv
	}
	return v
}

func (v *VolObj) AddConnection() error {
	log.Debugf("Incrementing connections for volume %s", v.Name)
	stmt := fmt.Sprintf("UPDATE %s SET %s = ?, %s = datetime('now', 'utc') WHERE %s = ?", VolumeTable, ConnKey, UpdateKey, NameKey)
	statement, err := _dbcon.Prepare(stmt)
	panicErr(err)
	v.Connections++
	_, err = statement.Exec(v.Connections, v.Name)
	panicErr(err)
	return nil
}

func (v *VolObj) DelConnection() error {
	log.Debugf("Decrementing connections for volume %s", v.Name)
	if v.Connections > 0 {
		v.Connections--
	} else {
		log.Debugf("Connections for volume %s is already 0", v.Name)
		v.Connections = 0
	}
	stmt := fmt.Sprintf("UPDATE %s SET %s = ?, %s = datetime('now', 'utc') WHERE %s = ?", VolumeTable, ConnKey, UpdateKey, NameKey)
	statement, err := _dbcon.Prepare(stmt)
	panicErr(err)
	_, err = statement.Exec(v.Connections, v.Name)
	panicErr(err)
	return nil
}

func (v *VolObj) ResetConnections() error {
	log.Debugf("Resetting connections for volume %s to 0", v.Name)
	stmt := fmt.Sprintf("UPDATE %s SET %s = ?, %s = datetime('now', 'utc') WHERE %s = ?", VolumeTable, ConnKey, UpdateKey, NameKey)
	statement, err := _dbcon.Prepare(stmt)
	panicErr(err)
	_, err = statement.Exec(0, v.Name)
	panicErr(err)
	return nil
}
