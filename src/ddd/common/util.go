package common

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"

	log "github.com/sirupsen/logrus"
)

// Binding this to an exported function for
// mocking purposes in tests
var (
	OS         ISystem
	FileReader ReadFile
)

// "OS" interface to allow for mocking purposes in tests
// If more OS functions are needed, just add them to this interface
// and the concrete implementation
type ISystem interface {
	Lstat(string) (os.FileInfo, error)
	Stat(string) (os.FileInfo, error)
	IsNotExist(error) bool
	MkdirAll(string, os.FileMode) error
}

// Concrete OS impelmentation
type System struct {
}

func (s System) Lstat(f string) (os.FileInfo, error) {
	return os.Lstat(f)
}

func (s System) Stat(f string) (os.FileInfo, error) {
	return os.Stat(f)
}

func (s System) IsNotExist(e error) bool {
	return os.IsNotExist(e)
}

func (s System) MkdirAll(f string, o os.FileMode) error {
	return os.MkdirAll(f, o)
}

type ReadFile func(f string) ([]byte, error)

func PanicErr(err error) {
	if err != nil {
		log.Error(err)
		panic(err)
	}
}

func ExecC(name string, arg ...string) *exec.Cmd {
	cmd := name + " " + strings.Join(arg, " ")
	log.Debugf("Executing Command: %s", cmd)
	return exec.Command(name, arg...)
}

func Prettify(v interface{}) string {
	b, _ := json.MarshalIndent(v, "", " ")
	return string(b)
}
