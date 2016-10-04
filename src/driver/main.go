package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/docker/go-plugins-helpers/volume"
)

const (
	dateraId      = "_datera"
	socketAddress = "/run/docker/plugins/datera.sock"
)

var (
	defaultDir  = filepath.Join(volume.DefaultDockerRootDirectory, dateraId)
	restAddress = flag.String("datera-cluster", "", "URL to datera api")
	dateraBase  = flag.String("datera-base", "/mnt/datera", "Base directory where volumes are created in the cluster")
	root        = flag.String("root", defaultDir, "datera volumes root directory")
	username    = flag.String("username", "", "Username for Datera backend account")
	password    = flag.String("password", "", "Password for Datera backend account")
	debug       = flag.Bool("debug", false, "Enable debug logging")
	version     = flag.Bool("version", false, "Print version info")
	noSsl       = flag.Bool("no-ssl", false, "Disable driver SSL")
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
	f, err := os.OpenFile("datera_docker_driver.log", os.O_APPEND|os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		fmt.Printf("error opening file: %v", err)
	}
	defer f.Close()
	log.SetOutput(f)
	log.Println(
		fmt.Sprintf("Options: root: %s, datera-cluster: %s, datera-base: %s, username: %s, password: %s",
			*root, *restAddress, *dateraBase, *username, "*******"))

	d := NewDateraDriver(*root, *restAddress, *dateraBase, *username, *password, *debug, *noSsl)
	h := volume.NewHandler(d)
	fmt.Printf("listening on %s\n", socketAddress)
	fmt.Println(h.ServeUnix("root", "datera"))
}
