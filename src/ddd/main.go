package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	dc "ddd/client"
	co "ddd/common"
	dd "ddd/driver"
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
	version   = flag.Bool("version", false, "Print version info")
	config    = flag.String("config", "", "Config File Location")
	genconfig = flag.String("genconfig", "", fmt.Sprintf("Generate Config Template. Options: 'bare', 'dcos-docker' and 'dcos-mesos'. Creates '%s' file", genConfigFile))
	printopts = flag.Bool("print-opts", false, "Print --opt supported values")
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

func PrintOpts() {
	klong := 0
	vlong := 0
	keys := []string{}
	for k, v := range dd.Opts {
		keys = append(keys, k)
		if len(k) > klong {
			klong = len(k)
		}
		if len(v[0]) > vlong {
			vlong = len(v[0])
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%s", k)
		fmt.Printf("%s", strings.Repeat(" ", klong-len(k))+"  --  ")
		fmt.Printf("%s", dd.Opts[k][0])
		fmt.Printf("%s", strings.Repeat(" ", vlong-len(dd.Opts[k][0])))
		fmt.Printf("%s", "  Default: ")
		fmt.Printf("%s\n", dd.Opts[k][1])
	}
}

func GenConfigBare() error {
	data := dc.Config{
		DateraCluster: "1.1.1.1",
		Username:      "my-user",
		Password:      "my-pass",
		Debug:         false,
		Ssl:           true,
		Tenant:        "/root",
		OsUser:        "root",
		Framework:     "bare",
	}

	return WriteConfig(&data)
}

func GenConfigDcosMesos() error {
	data := dc.Config{
		DateraCluster: "1.1.1.1",
		Username:      "my-user",
		Password:      "my-pass",
		Debug:         false,
		Ssl:           true,
		Tenant:        "/root",
		OsUser:        "root",
		Framework:     "dcos-mesos",
	}

	return WriteConfig(&data)
}

func GenConfigDcosDocker() error {
	// vdata := dc.VolOpts{
	// 	Size:          16,
	// 	Replica:       3,
	// 	PlacementMode: "hybrid",
	// 	MaxIops:       0,
	// 	MaxBW:         0,
	// 	Template:      "",
	// 	FsType:        "ext4",
	// 	Persistence:   "manual",
	// 	CloneSrc:      "",
	// }
	data := dc.Config{
		DateraCluster: "1.1.1.1",
		Username:      "my-user",
		Password:      "my-pass",
		Debug:         false,
		Ssl:           true,
		Tenant:        "/root",
		OsUser:        "root",
		Framework:     "dcos-docker",
	}

	return WriteConfig(&data)
}

func WriteConfig(data *dc.Config) error {
	j, err := json.MarshalIndent(&data, "", "    ")
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(genConfigFile, j, 0644)
	return err
}

func ParseConfig(file string) (*dc.Config, error) {
	var conf dc.Config
	data, err := ioutil.ReadFile(file)
	if err != nil {
		fmt.Println(err)
		return &conf, err
	}
	err = json.Unmarshal(data, &conf)
	if err != nil {
		return &conf, err
	}
	// handle setting defaults
	if conf.Tenant == "" {
		conf.Tenant = "/root"
	}
	if conf.OsUser == "" {
		conf.OsUser = "root"
	}
	if len(conf.DateraCluster) == 0 {
		err = fmt.Errorf("Invalid 'datera-cluster' config value: %s", conf.DateraCluster)
		return &conf, err
	}
	if len(conf.Username) == 0 {
		err = fmt.Errorf("Invalid 'username' config value: %s", conf.Username)
		return &conf, err
	}
	return &conf, nil
}

func main() {
	log.AddHook(stack.StandardHook())
	flag.Parse()
	if *version {
		fmt.Printf("Version: %s\n", dd.DRIVER+"-"+dd.DriverVersion)
		os.Exit(0)
	}

	if *printopts {
		PrintOpts()
		os.Exit(0)
	}

	if *genconfig == "bare" {
		if err := GenConfigBare(); err != nil {
			fmt.Println(err)
		}
		os.Exit(0)
	} else if *genconfig == "dcos-docker" {
		if err := GenConfigDcosDocker(); err != nil {
			fmt.Println(err)
		}
		os.Exit(0)
	} else if *genconfig == "dcos-mesos" {
		if err := GenConfigDcosMesos(); err != nil {
			fmt.Println(err)
		}
		os.Exit(0)
	}
	ctxt := co.MkCtxt("Main")

	if *config == "" {
		usr, err := user.Current()
		if err != nil {
			co.Fatal(ctxt, "Couldn't determine current user")
		}
		*config = path.Join(usr.HomeDir, defaultConfigFile)
	}

	conf, err := ParseConfig(*config)
	if err != nil {
		Usage()
		co.Fatal(ctxt, err)
		os.Exit(1)
	}

	co.Debugf(ctxt, "Options: datera-cluster: %s, username: %s, password: %s", conf.DateraCluster, conf.Username, "*******")

	d := dd.NewDateraDriver(conf)
	h := dv.NewHandler(d)
	u, err := user.Lookup(conf.OsUser)
	if err != nil {
		co.Errorf(ctxt, "Could not look up GID for user %s", conf.OsUser)
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
		co.LogUploadDaemon(conf.DateraCluster, conf.Username, conf.Password, "datera-ddd.bin", 60)
	}()

	co.Debugf(ctxt, "listening on %s.sock\n", sockName)
	co.Debug(ctxt, h.ServeUnix(sockName, gid))
}
