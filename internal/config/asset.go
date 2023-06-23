// Code generated for package config by go-bindata DO NOT EDIT. (@generated)
// sources:
// configs/banner.txt
// configs/default.yaml
// test/client/configs/default.yaml
package config

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func bindataRead(data []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("Read %q: %v", name, err)
	}

	var buf bytes.Buffer
	_, err = io.Copy(&buf, gz)
	clErr := gz.Close()

	if err != nil {
		return nil, fmt.Errorf("Read %q: %v", name, err)
	}
	if clErr != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

type asset struct {
	bytes []byte
	info  os.FileInfo
}

type bindataFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

// Name return file name
func (fi bindataFileInfo) Name() string {
	return fi.name
}

// Size return file size
func (fi bindataFileInfo) Size() int64 {
	return fi.size
}

// Mode return file mode
func (fi bindataFileInfo) Mode() os.FileMode {
	return fi.mode
}

// Mode return file modify time
func (fi bindataFileInfo) ModTime() time.Time {
	return fi.modTime
}

// IsDir return file whether a directory
func (fi bindataFileInfo) IsDir() bool {
	return fi.mode&os.ModeDir != 0
}

// Sys return file is sys mode
func (fi bindataFileInfo) Sys() interface{} {
	return nil
}

var _configsBannerTxt = []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\xff\xa4\x4f\x41\xaa\x44\x21\x0c\xdb\x7b\x8a\x2c\x15\x3e\xf4\x42\x42\xfe\x41\x7a\xf8\xa1\x4d\x2b\xe2\x72\x26\x60\x4c\x3a\x69\x9c\x07\x81\x20\x01\xf2\xe5\xaf\x30\x74\x4d\x2e\x18\x67\x34\x2f\x0b\xde\xd2\xf5\x62\xa1\x55\x8f\xce\xd5\xa1\xf2\x2a\x35\x44\x53\xb6\xa5\x96\x07\x3c\x43\xa1\x83\xb0\x4b\xb9\x02\xed\x4f\x08\xff\xfa\x43\xa3\x3b\x8d\x75\x6e\xce\xf8\x8c\xfa\x85\x7c\xd5\x00\x38\x3c\x45\xf9\xee\xbc\xd6\x86\x7e\xdb\x64\x9c\x87\xf5\x15\x67\x60\x9a\xf8\xe3\x13\x9b\xfc\x43\xaf\x0d\x3b\xdb\xbf\xc0\x78\xd7\x7c\x02\x00\x00\xff\xff\x45\xef\x01\xe6\xfd\x01\x00\x00")

func configsBannerTxtBytes() ([]byte, error) {
	return bindataRead(
		_configsBannerTxt,
		"configs/banner.txt",
	)
}

func configsBannerTxt() (*asset, error) {
	bytes, err := configsBannerTxtBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "configs/banner.txt", size: 509, mode: os.FileMode(420), modTime: time.Unix(1678162039, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _configsDefaultYaml = []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\xff\x6c\xd0\xc1\x4a\x03\x31\x10\x06\xe0\xfb\x3e\xc5\x90\x7b\x77\xb7\x82\x18\x72\x6b\x69\x15\x44\xb0\x28\x3e\x40\x1a\x67\xd3\xe8\x34\xb3\x24\xb3\xa5\xfa\xf4\x12\xa3\xb8\xa2\xc7\x7c\xff\x64\x7e\x12\x62\x6f\x1a\x00\xc7\x31\x33\xe1\x36\xda\x3d\xa1\x01\x49\x13\x36\x00\x43\xf8\x43\x63\x0a\x51\x56\xf9\x36\x73\x34\x30\x58\xca\x05\x89\xfd\x1d\x9e\x90\x0c\xa8\xcd\x76\xfd\x74\xa3\xaa\x6d\x42\x42\x27\x9c\xde\x0c\xa8\xb6\x23\xf6\xb9\xfb\x4a\xae\x43\x59\xa9\x5e\x44\xf7\x7a\x91\x31\x9d\x30\x2d\x3c\xb7\xc4\xbe\x0c\x1c\xed\xf9\x31\xbc\xe3\xfd\xf0\xc0\x44\x21\x7a\x03\x97\x7d\xe5\xb5\x75\xaf\xd3\x98\x67\xc9\xf2\x42\xd7\x68\xe5\xe7\x17\xae\x9a\xa6\xae\x2d\x8f\x8b\xf6\xf8\x4f\x5b\x69\x1a\x39\x49\x99\x00\x10\x37\xee\xca\x01\x94\xee\x75\xaf\x3e\x6d\x7a\x9e\xd9\xb2\xda\x41\xe4\x07\x7b\x5d\x70\x6f\x63\xac\x45\x00\xf8\xfb\xb7\xbe\xc3\x9d\x95\x83\x01\xe5\x38\x0e\xc1\xe7\xae\x62\x2b\x67\x51\xcd\x47\x00\x00\x00\xff\xff\x4d\x9b\x24\x20\x81\x01\x00\x00")

func configsDefaultYamlBytes() ([]byte, error) {
	return bindataRead(
		_configsDefaultYaml,
		"configs/default.yaml",
	)
}

func configsDefaultYaml() (*asset, error) {
	bytes, err := configsDefaultYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "configs/default.yaml", size: 385, mode: os.FileMode(420), modTime: time.Unix(1678936086, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _testClientConfigsDefaultYaml = []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\xff\x84\x94\xbd\x72\xe3\x36\x10\xc7\x7b\x3d\x05\x86\xae\x25\x13\xb2\x64\x51\xe8\xe4\xcf\x71\xc6\x33\xf1\xd8\x4e\x8a\x68\x5c\xc0\xe0\x0a\x46\x04\x61\x39\x00\xa8\x89\x12\xfb\x0d\xe2\x22\x45\xba\xbb\xb9\xf6\x8a\x2b\xaf\xba\x8f\xb9\x97\xb1\xdd\xde\x2b\xdc\x90\x10\x75\x92\x2d\xca\x25\xf7\xf7\xdf\xff\x02\x8b\x5d\x6a\x94\xac\x41\x88\x40\xe3\x50\xc3\xa1\xe1\xd7\x1a\x18\xf1\x36\x87\x06\x21\x23\xf5\x22\x94\x59\x65\xfc\xc0\xfd\xe2\xd0\x30\x32\xe2\xda\x15\x41\x8d\xf2\x14\xa6\xa0\x19\x89\x0e\x0e\xf7\x7e\x3b\x8e\x42\xec\x40\x59\x10\x1e\xed\x8c\x91\xa8\xb5\xad\x51\xba\xed\x39\x39\x52\x85\x65\xf4\xa7\x4f\xe2\xa4\x29\xb4\x02\xe3\x9b\x12\x5b\x1a\x65\x21\x98\xf0\xbf\x2e\xd4\xdf\xf0\xeb\xe8\x1c\xb5\x56\x46\x32\xd2\x8d\x43\x78\x8f\x8b\x71\x9e\xb9\x25\x42\xdb\x49\x40\x03\xb9\x9c\xd0\x6b\x04\xd7\xe2\x6e\x86\x4f\xd6\x14\x8b\xc2\xad\x45\x6e\x2d\x18\x31\x63\x84\x86\x80\x29\x52\x08\xd9\x22\x16\x26\xe8\x61\x90\xa6\x96\x91\x48\xa3\xe0\xfa\x06\x9d\x67\x49\x9c\xc4\xd1\x3a\x09\xa5\xfd\x56\xbb\xd3\x6f\x75\xfa\x2d\x4a\x63\xd6\x5d\xe8\xd6\xaa\xba\xb4\x45\x77\x77\x17\xaa\x14\xa6\x4a\x40\x28\xad\xd2\x73\x90\x8c\x44\xc3\xb8\xd9\xbf\xfa\xa7\x77\x17\x5c\xd4\x04\xd4\x72\x9c\x76\xef\xaa\x63\x64\x37\x68\x20\x30\xba\x13\x07\x9c\xcc\xe9\x2a\xa3\xed\x9d\x4e\x77\xb7\x97\x2c\x32\x35\xf7\x73\xfa\xf0\xe9\xc3\x60\x38\x68\xfe\x51\x66\x57\xde\xcf\x78\x99\x3e\x27\x16\x3d\x0a\xd4\xbf\x83\x75\xaa\x98\x85\xa8\x1d\xd3\x7e\x60\xde\x72\xe3\xce\x0a\x01\x23\xd1\xe5\xfe\x59\x88\x8e\x01\x32\xae\xd5\x14\x18\x69\xc7\x64\x8b\x3c\x7c\x7b\xfb\xf4\xf1\xf3\xe3\x7f\xef\x9f\xde\xbc\xfb\xfe\xe5\xdf\xc7\xfb\xff\x1f\xbe\xde\xbb\xca\x7c\xaa\x8c\x80\x93\x95\x4e\xb4\xe7\x87\x12\xca\xcf\x56\x49\x67\xf9\xb8\xfb\xa8\xd1\x56\xb4\x3c\x71\xd1\xad\xbb\x9f\x5d\x3e\x06\x0c\x8d\x2e\x1e\xd5\x2b\x34\xe7\x90\xa1\xf5\x27\xc6\x83\x9d\x72\xcd\x08\x8d\x4b\x2c\x2b\x1d\x21\x5c\x88\x0b\xcf\x7d\xee\x82\x6f\x7c\x4b\xa3\x39\xa9\x3c\xea\x30\xf7\xca\xe7\x29\x5c\xce\x32\x58\x93\x6b\x64\x3d\xc5\x0c\x2c\xf7\xca\xc8\x1a\x6b\x09\x78\x68\x84\x9d\x65\x9b\xca\x23\x4f\x6b\xd0\x51\x0e\xfa\x62\xe6\x3c\x4c\x6a\x04\x03\xed\xc1\x1a\xee\xd1\x6e\x94\x1d\x20\xda\x53\x14\x63\xa8\x2b\x34\xb2\x68\x7c\xa1\xaa\xe1\x13\x95\x6e\xa0\xd7\x5c\x8c\x37\xe0\xd4\xaa\x29\xd8\x0d\x02\x91\x3b\x8f\x93\x0d\x02\x99\xb9\xd3\xcd\x6f\x78\x0d\x2a\xc5\xfc\x15\x91\xd4\x68\xb8\x7b\xcd\x4a\x72\xad\x34\xe0\x2b\xaa\xe2\x52\x75\xef\x5e\xcd\x1b\x7b\x36\x60\x8b\x75\x48\xae\xca\x9d\xb8\xed\xc7\x2f\xc6\x6c\x75\x99\x6e\xe9\x30\x6e\xf6\xe6\x6a\x9a\x2c\xe4\x5c\xfb\x17\xea\x6a\xc1\xca\x76\x57\xa5\x5d\x06\xb0\x76\x41\x09\x49\xcb\x3f\x7f\xb9\x5a\x2b\x25\x87\xb4\xd9\xbe\x5a\x7c\xed\x0c\xe3\x66\x37\x7c\x46\x8d\x1f\x01\x00\x00\xff\xff\xc7\xcb\xf6\x42\x89\x06\x00\x00")

func testClientConfigsDefaultYamlBytes() ([]byte, error) {
	return bindataRead(
		_testClientConfigsDefaultYaml,
		"test/client/configs/default.yaml",
	)
}

func testClientConfigsDefaultYaml() (*asset, error) {
	bytes, err := testClientConfigsDefaultYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "test/client/configs/default.yaml", size: 1673, mode: os.FileMode(420), modTime: time.Unix(1687527729, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

// Asset loads and returns the asset for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func Asset(name string) ([]byte, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("Asset %s can't read by error: %v", name, err)
		}
		return a.bytes, nil
	}
	return nil, fmt.Errorf("Asset %s not found", name)
}

// MustAsset is like Asset but panics when Asset would return an error.
// It simplifies safe initialization of global variables.
func MustAsset(name string) []byte {
	a, err := Asset(name)
	if err != nil {
		panic("asset: Asset(" + name + "): " + err.Error())
	}

	return a
}

// AssetInfo loads and returns the asset info for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func AssetInfo(name string) (os.FileInfo, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("AssetInfo %s can't read by error: %v", name, err)
		}
		return a.info, nil
	}
	return nil, fmt.Errorf("AssetInfo %s not found", name)
}

// AssetNames returns the names of the assets.
func AssetNames() []string {
	names := make([]string, 0, len(_bindata))
	for name := range _bindata {
		names = append(names, name)
	}
	return names
}

// _bindata is a table, holding each asset generator, mapped to its name.
var _bindata = map[string]func() (*asset, error){
	"configs/banner.txt":               configsBannerTxt,
	"configs/default.yaml":             configsDefaultYaml,
	"test/client/configs/default.yaml": testClientConfigsDefaultYaml,
}

// AssetDir returns the file names below a certain
// directory embedded in the file by go-bindata.
// For example if you run go-bindata on data/... and data contains the
// following hierarchy:
//     data/
//       foo.txt
//       img/
//         a.png
//         b.png
// then AssetDir("data") would return []string{"foo.txt", "img"}
// AssetDir("data/img") would return []string{"a.png", "b.png"}
// AssetDir("foo.txt") and AssetDir("notexist") would return an error
// AssetDir("") will return []string{"data"}.
func AssetDir(name string) ([]string, error) {
	node := _bintree
	if len(name) != 0 {
		cannonicalName := strings.Replace(name, "\\", "/", -1)
		pathList := strings.Split(cannonicalName, "/")
		for _, p := range pathList {
			node = node.Children[p]
			if node == nil {
				return nil, fmt.Errorf("Asset %s not found", name)
			}
		}
	}
	if node.Func != nil {
		return nil, fmt.Errorf("Asset %s not found", name)
	}
	rv := make([]string, 0, len(node.Children))
	for childName := range node.Children {
		rv = append(rv, childName)
	}
	return rv, nil
}

type bintree struct {
	Func     func() (*asset, error)
	Children map[string]*bintree
}

var _bintree = &bintree{nil, map[string]*bintree{
	"configs": &bintree{nil, map[string]*bintree{
		"banner.txt":   &bintree{configsBannerTxt, map[string]*bintree{}},
		"default.yaml": &bintree{configsDefaultYaml, map[string]*bintree{}},
	}},
	"test": &bintree{nil, map[string]*bintree{
		"client": &bintree{nil, map[string]*bintree{
			"configs": &bintree{nil, map[string]*bintree{
				"default.yaml": &bintree{testClientConfigsDefaultYaml, map[string]*bintree{}},
			}},
		}},
	}},
}}

// RestoreAsset restores an asset under the given directory
func RestoreAsset(dir, name string) error {
	data, err := Asset(name)
	if err != nil {
		return err
	}
	info, err := AssetInfo(name)
	if err != nil {
		return err
	}
	err = os.MkdirAll(_filePath(dir, filepath.Dir(name)), os.FileMode(0755))
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(_filePath(dir, name), data, info.Mode())
	if err != nil {
		return err
	}
	err = os.Chtimes(_filePath(dir, name), info.ModTime(), info.ModTime())
	if err != nil {
		return err
	}
	return nil
}

// RestoreAssets restores an asset under the given directory recursively
func RestoreAssets(dir, name string) error {
	children, err := AssetDir(name)
	// File
	if err != nil {
		return RestoreAsset(dir, name)
	}
	// Dir
	for _, child := range children {
		err = RestoreAssets(dir, filepath.Join(name, child))
		if err != nil {
			return err
		}
	}
	return nil
}

func _filePath(dir, name string) string {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	return filepath.Join(append([]string{dir}, strings.Split(cannonicalName, "/")...)...)
}
