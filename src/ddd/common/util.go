package common

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"text/template"

	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
)

const (
	ReqName = "req"
	TraceId = "tid"
)

// Binding this to an exported function for
// mocking purposes in tests
var (
	OS         ISystem
	FileReader ReadFile
	host, _    = os.Hostname()
	topctxt    = context.WithValue(context.Background(), "host", host)
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

func ExecC(ctxt context.Context, name string, arg ...string) *exec.Cmd {
	cmd := name + " " + strings.Join(arg, " ")
	Debugf(ctxt, "Executing Command: %s", cmd)
	return exec.Command(name, arg...)
}

func Prettify(v interface{}) string {
	b, _ := json.MarshalIndent(v, "", " ")
	return string(b)
}

func Unpack(b []byte, m *map[string]interface{}) error {
	return json.Unmarshal(b, m)
}

func Tsprint(s string, m map[string]string) (string, error) {
	tpl, err := template.New("format").Parse(s)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	err = tpl.Execute(&buf, m)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

func GenId() string {
	return uuid.Must(uuid.NewV4()).String()
}

func MkCtxt(reqName string) context.Context {
	ctxt := context.WithValue(topctxt, TraceId, GenId())
	ctxt = context.WithValue(ctxt, ReqName, reqName)
	return ctxt
}

func Debug(ctxt context.Context, s interface{}) {
	reqname := ctxt.Value(ReqName).(string)
	tid := ctxt.Value(TraceId).(string)
	log.WithFields(log.Fields{
		ReqName: reqname,
		TraceId: tid,
	}).Debug(s)
}

func Debugf(ctxt context.Context, s string, args ...interface{}) {
	checkArgs(ctxt, s, args...)
	reqname := ctxt.Value(ReqName).(string)
	tid := ctxt.Value(TraceId).(string)
	log.WithFields(log.Fields{
		ReqName: reqname,
		TraceId: tid,
	}).Debugf(s, args...)
}

func Info(ctxt context.Context, s interface{}) {
	reqname := ctxt.Value(ReqName).(string)
	tid := ctxt.Value(TraceId).(string)
	log.WithFields(log.Fields{
		ReqName: reqname,
		TraceId: tid,
	}).Info(s)
}

func Infof(ctxt context.Context, s string, args ...interface{}) {
	checkArgs(ctxt, s, args...)
	reqname := ctxt.Value(ReqName).(string)
	tid := ctxt.Value(TraceId).(string)
	log.WithFields(log.Fields{
		ReqName: reqname,
		TraceId: tid,
	}).Infof(s, args...)
}

func Warning(ctxt context.Context, s interface{}) {
	reqname := ctxt.Value(ReqName).(string)
	tid := ctxt.Value(TraceId).(string)
	log.WithFields(log.Fields{
		ReqName: reqname,
		TraceId: tid,
	}).Warning(s)
}

func Warningf(ctxt context.Context, s string, args ...interface{}) {
	checkArgs(ctxt, s, args...)
	reqname := ctxt.Value(ReqName).(string)
	tid := ctxt.Value(TraceId).(string)
	log.WithFields(log.Fields{
		ReqName: reqname,
		TraceId: tid,
	}).Warningf(s, args...)
}

func Errorf(ctxt context.Context, s string, args ...interface{}) {
	checkArgs(ctxt, s, args...)
	reqname := ctxt.Value(ReqName).(string)
	tid := ctxt.Value(TraceId).(string)
	log.WithFields(log.Fields{
		ReqName: reqname,
		TraceId: tid,
	}).Errorf(s, args...)
}

func Error(ctxt context.Context, s interface{}) {
	reqname := ctxt.Value(ReqName).(string)
	tid := ctxt.Value(TraceId).(string)
	log.WithFields(log.Fields{
		ReqName: reqname,
		TraceId: tid,
	}).Error(s)
}

func Fatal(ctxt context.Context, s interface{}) {
	reqname := ctxt.Value(ReqName).(string)
	tid := ctxt.Value(TraceId).(string)
	log.WithFields(log.Fields{
		ReqName: reqname,
		TraceId: tid,
	}).Fatal(s)
}

func Fatalf(ctxt context.Context, s string, args ...interface{}) {
	checkArgs(ctxt, s, args...)
	reqname := ctxt.Value(ReqName).(string)
	tid := ctxt.Value(TraceId).(string)
	log.WithFields(log.Fields{
		ReqName: reqname,
		TraceId: tid,
	}).Fatalf(s, args...)
}

// Hack just to make sure I don't miss these
func checkArgs(ctxt context.Context, s string, args ...interface{}) {
	c := 0
	for _, f := range []string{"%s", "%d", "%v", "%#v", "%t", "%p"} {
		c += strings.Count(s, f)
	}
	l := len(args)
	if c != l {
		Warningf(ctxt, "Wrong number of args for format string, [%d != %d]", l, c)
	}
}
