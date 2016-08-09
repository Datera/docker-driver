package main

import (
	"flag"
	"fmt"
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
)

func main() {
	var Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options]\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.Parse()
	if len(*restAddress) == 0 {
		Usage()
		os.Exit(1)
	}

	d := newDateraDriver(*root, *restAddress, *dateraBase)
	h := volume.NewHandler(d)
	fmt.Printf("listening on %s\n", socketAddress)
	fmt.Println(h.ServeUnix("root", "datera"))
}
