package main

import (
	"flag"
	"fmt"
	"os"
	"os/user"
	"strconv"
	"time"

	co "github.com/Datera/docker-driver/pkg/common"
	dd "github.com/Datera/docker-driver/pkg/driver"
	udc "github.com/Datera/go-udc/pkg/udc"

	dv "github.com/docker/go-plugins-helpers/volume"
	stack "github.com/gurpartap/logrus-stack"
	log "github.com/sirupsen/logrus"
)

const (
	sockName          = "datera"
	defaultConfigFile = ".datera-config-file"
	genConfigFile     = "datera-config-template.txt"
)

/*
Config File Format: json
Use -genconfig option to generate this file

{
	"datera-cluster": "1.1.1.1",
	"username": "my-user",
	"password": "my-pass",
	"debug": false,
	"ssl": true,
	"tenant": "/root",
	"os-user": "root"
}

*/

var (
	version = flag.Bool("version", false, "Print version info")
)

func Usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s [options]\n", os.Args[0])
	flag.PrintDefaults()
	msg := `
A config file must either be specified via
the '-config' opt or a '.datera-config-file'
can be placed in your user directory.  Use
the '-genconfig' opt to generate a config
file template in this directory
`
	fmt.Fprintf(os.Stderr, "%s", msg)
}

func main() {
	log.AddHook(stack.StandardHook())
	flag.Parse()
	if *version {
		fmt.Printf("Version: %s\n", dd.DRIVER+"-"+dd.DriverVersion)
		os.Exit(0)
	}

	ctxt := co.MkCtxt("Main")

	conf, err := udc.GetConfig()
	if err != nil {
		log.Fatal(err)
	}
	log.Info("Using Universal Datera Config")
	udc.PrintConfig()

	d := dd.NewDateraDriver(conf)
	h := dv.NewHandler(d)
	u, err := user.Current()
	if err != nil {
		co.Errorf(ctxt, "Could not look up GID for user %s", u)
		os.Exit(2)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		co.Errorf(ctxt, "Could not convert gid to int: %s", u.Gid)
		os.Exit(3)
	}
	// Start log daemon process after an initial sleep
	go func() {
		time.Sleep(120 * time.Second)
		co.LogUploadDaemon(conf.MgmtIp, conf.Username, conf.Password, "datera-ddd.bin", 60)
	}()

	co.Debugf(ctxt, "listening on %s.sock\n", sockName)
	co.Debug(ctxt, h.ServeUnix(sockName, gid))
}
