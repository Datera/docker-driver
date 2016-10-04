package main_test

import (
	"driver"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/docker/go-plugins-helpers/volume"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const (
	testDefaultDir = "/home/testuser/somefolder"
	testRESTAddr   = "http://testmachine.com:8888"
	testDateraBase = "/mnt/datera"
	testUsername   = "test-user"
	testPassword   = "12345678"
	testDebug      = true
	testNoSSL      = false
)

var (
	testRequest = volume.Request{
		Name:    "testRequest",
		Options: makeOpts("42", "3", "testtemp", "testfstype", "100", "101")}
	testMountRequest = volume.MountRequest{
		Name: "testMountRequest"}
)

///////////////////////////////////////////////////////
// Mock the DateraClient object for testing the driver
///////////////////////////////////////////////////////
type MockDateraClient struct {
	mock.Mock
	addr     string
	base     string
	username string
	password string
}

func (m *MockDateraClient) Login(name, password string) error {
	args := m.Called(name)
	return args.Error(0)
}

func (m *MockDateraClient) VolumeExist(name string) (bool, error) {
	args := m.Called(name)
	return args.Bool(0), args.Error(1)
}
func (m *MockDateraClient) CreateVolume(
	name string,
	size uint64,
	replica uint8,
	template string,
	maxIops uint64,
	maxBW uint64) error {
	args := m.Called(name)
	return args.Error(0)
}
func (m *MockDateraClient) StopVolume(name string) error {
	args := m.Called(name)
	return args.Error(0)
}
func (m *MockDateraClient) MountVolume(name string, destination string, fsType string) error {
	args := m.Called(name)
	return args.Error(0)
}
func (m *MockDateraClient) UnmountVolume(name string, destination string) error {
	args := m.Called(name)
	return args.Error(0)
}
func (m *MockDateraClient) DetachVolume(name string) error {
	args := m.Called(name)
	return args.Error(0)
}
func (m *MockDateraClient) GetIQNandPortal(name string) (string, string, string, error) {
	args := m.Called(name)
	return "", "", "", args.Error(0)
}

///////////////////////////////////////////////////////

func makeOpts(size, replica, template, fstype, maxiops, maxbw string) map[string]string {
	testOptions := map[string]string{
		"size":     size,
		"replica":  replica,
		"template": template,
		"fstype":   fstype,
		"maxiops":  maxiops,
		"maxbw":    maxbw,
	}
	return testOptions
}

func mockSetup(t *testing.T) (*assert.Assertions, main.DateraDriver) {
	assert := assert.New(t)

	mockClient := new(MockDateraClient)
	mockClient.On("VolumeExist", mock.Anything).Return(true, nil)
	mockClient.On("StopVolume", mock.Anything).Return(nil)

	d := main.NewDateraDriver(testDefaultDir,
		testRESTAddr,
		testDateraBase,
		testUsername,
		testPassword,
		testDebug,
		testNoSSL)

	d.DateraClient = mockClient

	return assert, d
}

func TestDriverConstructor(t *testing.T) {
	assert := assert.New(t)

	var d interface{} = main.NewDateraDriver(testDefaultDir,
		testRESTAddr,
		testDateraBase,
		testUsername,
		testPassword,
		testDebug,
		testNoSSL)

	_, ok := d.(main.DateraDriver)

	e := fmt.Sprintf("Constructor Returned Incorrect Type: %T", d)
	assert.True(ok, e)
}

func TestDriverVersion(t *testing.T) {
	assert, d := mockSetup(t)
	assert.NotNil(d.GetVersion())
}

func TestDriverCreate(t *testing.T) {
	assert, d := mockSetup(t)
	enddir := filepath.Join(testDefaultDir, testRequest.Name)

	result := d.Create(testRequest)
	assert.Equal(volume.Response{}, result)
	vmap := d.GetVolumeMap()
	assert.Equal(len(vmap), 1)
	assert.NotNil(vmap[enddir])
}

func TestDriverRemove(t *testing.T) {
	assert, d := mockSetup(t)
	enddir := filepath.Join(testDefaultDir, testRequest.Name)

	d.Create(testRequest)

	result := d.Remove(testRequest)
	assert.Equal(volume.Response{}, result)
	vmap := d.GetVolumeMap()
	assert.Equal(len(vmap), 0)
	assert.Nil(vmap[enddir])
}

func TestDriverList(t *testing.T) {
	assert, d := mockSetup(t)
	result := d.List(testRequest)
	enddir := filepath.Join(testDefaultDir, testRequest.Name)

	assert.Equal(volume.Response{Volumes: nil}, result)

	d.Create(testRequest)
	vols := []*volume.Volume{}
	vols = append(vols, &volume.Volume{Name: testRequest.Name, Mountpoint: enddir})
	result = d.List(testRequest)
	assert.Equal(volume.Response{Volumes: vols}, result)
}

func TestDriverGet(t *testing.T) {
	assert, d := mockSetup(t)
	result := d.Get(testRequest)
	enddir := filepath.Join(testDefaultDir, testRequest.Name)
	assert.Equal(volume.Response{Err: fmt.Sprintf("Unable to find volume mounted on %#v", enddir)}, result)

	d.Create(testRequest)
	result = d.Get(testRequest)
	assert.Equal(volume.Response{Volume: &volume.Volume{Name: testRequest.Name, Mountpoint: enddir}}, result)
}

func TestDriverPath(t *testing.T) {
	assert, d := mockSetup(t)
	result := d.Path(testRequest)
	enddir := filepath.Join(testDefaultDir, testRequest.Name)
	assert.Equal(volume.Response{Mountpoint: enddir}, result)
}
