package main

import (
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	log "github.com/Sirupsen/logrus"
	dv "github.com/docker/go-plugins-helpers/volume"
)

const (
	dateraId      = "_datera"
	socketAddress = "/run/docker/plugins/datera.sock"
	logFile       = "ddd.log"
)

var (
	defaultDir  = filepath.Join(dv.DefaultDockerRootDirectory, dateraId)
	restAddress = flag.String("datera-cluster", "", "URL to datera api")
	dateraBase  = flag.String("datera-base", "/mnt/datera", "Base directory where volumes are created in the cluster")
	root        = flag.String("root", defaultDir, "datera volumes root directory")
	username    = flag.String("username", "", "Username for Datera backend account")
	password    = flag.String("password", "", "Password for Datera backend account")
	debug       = flag.Bool("debug", false, "Enable debug logging")
	version     = flag.Bool("version", false, "Print version info")
	noSsl       = flag.Bool("no-ssl", false, "Disable driver SSL")
	tenant      = flag.String("tenant", "root", "Tenant requests should use")
	osUser      = flag.String("os-user", "root", "User which this process should run under")
)

func main() {
	flag.Parse()
	if *version {
		fmt.Printf("Version: %s\n", DRIVER+"-"+DriverVersion)
		os.Exit(0)
	}

	var Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n", os.Args[0])
		flag.PrintDefaults()
	}
	if len(*restAddress) == 0 {
		Usage()
		os.Exit(1)
	}

	// Create log file
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		fmt.Printf("error opening file: %v", err)
	}
	defer f.Close()
	log.SetOutput(f)
	log.Infof(
		"Options: root: %s, datera-cluster: %s, datera-base: %s, username: %s, password: %s",
		*root, *restAddress, *dateraBase, *username, "*******")

	d := NewDateraDriver(*root, *restAddress, *dateraBase, *username, *password, *tenant, *debug, *noSsl)
	h := dv.NewHandler(d)
	fmt.Printf("listening on %s\n", socketAddress)
	u, err := user.Lookup(*osUser)
	if err != nil {
		log.Errorf("Could not look up GID for user %s", *osUser)
		os.Exit(2)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		log.Errorf("Could not convert gid to int: %s", u.Gid)
		os.Exit(2)
	}
	fmt.Println(h.ServeUnix("datera", gid))
}
