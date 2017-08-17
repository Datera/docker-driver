package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"path"
	"strconv"

	dv "github.com/docker/go-plugins-helpers/volume"
	stack "github.com/gurpartap/logrus-stack"
	log "github.com/sirupsen/logrus"
)

const (
	sockName          = "datera"
	logFile           = "ddd.log"
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
	genconfig = flag.Bool("genconfig", false, fmt.Sprintf("Generate Config Template. Creates '%s' file", genConfigFile))
	showenvs  = flag.Bool("show-envs", false, "Print the supported environment variables for use with Docker under DCOS")
)

type Config struct {
	DateraCluster string `json:"datera-cluster"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	Ssl           bool   `json:"ssl"`
	Tenant        string `json:"tenant,omitempty"`
	OsUser        string `json:"os-user,omitempty"`
	Debug         bool   `json:"debug,omitempty"`
}

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

func GenConfig() error {
	data := Config{
		DateraCluster: "1.1.1.1",
		Username:      "my-user",
		Password:      "my-pass",
		Debug:         false,
		Ssl:           true,
		Tenant:        "/root",
		OsUser:        "root",
	}

	j, err := json.MarshalIndent(&data, "", "    ")
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(genConfigFile, j, 0644)
	return err
}

func ParseConfig(file string) (*Config, error) {
	var conf Config
	data, err := ioutil.ReadFile(file)
	if err != nil {
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
		fmt.Printf("Version: %s\n", DRIVER+"-"+DriverVersion)
		os.Exit(0)
	}

	if *showenvs {
		fmt.Println(FwkEnvVar, ": Datera framework.  Set to 'dcos' if running under DC/OS")
		fmt.Println(SizeEnvVar, ": Datera volume size")
		fmt.Println(ReplicaEnvVar, ": Datera volume replica count")
		fmt.Println(PlacementEnvVar, ": Datera volume placement mode")
		fmt.Println(MaxIopsEnvVar, ": Datera volume max iops")
		fmt.Println(MaxBWEnvVar, ": Datera volume max bandwidth")
		fmt.Println(TemplateEnvVar, ": Datera volume template")
		fmt.Println(FsTypeEnvVar, ": Datera volume filesystem, eg: ext4")
		os.Exit(0)
	}

	if *genconfig {
		GenConfig()
		os.Exit(0)
	}

	if *config == "" {
		usr, err := user.Current()
		if err != nil {
			log.Fatal("Couldn't determine current user")
		}
		*config = path.Join(usr.HomeDir, defaultConfigFile)
	}

	conf, err := ParseConfig(*config)
	if err != nil {
		Usage()
		log.Fatalf("%s", err)
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
		"Options: datera-cluster: %s, username: %s, password: %s",
		conf.DateraCluster, conf.Username, "*******")

	// Overriding these so tests can replace them
	OS = System{}
	FileReader = ioutil.ReadFile

	d := NewDateraDriver(conf.DateraCluster, conf.Username, conf.Password, conf.Tenant, conf.Debug, !conf.Ssl)
	h := dv.NewHandler(d)
	u, err := user.Lookup(conf.OsUser)
	if err != nil {
		log.Errorf("Could not look up GID for user %s", conf.OsUser)
		os.Exit(2)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		log.Errorf("Could not convert gid to int: %s", u.Gid)
		os.Exit(3)
	}
	log.Debugf("listening on %s.sock\n", sockName)
	log.Debug(h.ServeUnix(sockName, gid))
}
