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

var _testClientConfigsDefaultYaml = []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\xff\x84\x94\xbd\x72\xdb\x38\x10\xc7\x7b\x3e\x05\x86\xae\x25\x93\x92\x75\xa2\xd0\xc9\x9f\xe3\x1b\xcf\x9c\xc7\xf6\x5d\x71\x1a\x17\x30\xb8\x82\x11\x41\x58\x0e\x00\x6a\xa2\xc4\x7e\x83\xb8\x48\x91\x2e\x99\xb4\x29\x52\xa6\xca\xc7\xe4\x65\x6c\xb7\x79\x85\x0c\x09\x52\x91\x6c\x53\x2e\xb9\xbf\xff\xfe\x17\xd8\xc5\x52\xa1\xa0\x01\x21\x1c\xb5\x45\x05\x7b\x9a\x5d\x28\xa0\xc4\x99\x1c\x02\x42\xc6\xf2\x51\x28\x33\x52\xbb\xa1\xfd\xdb\xa2\xa6\x64\xcc\x94\x2d\x82\x0a\xc5\x11\xcc\x40\x51\x12\xee\xee\x6d\xff\x7b\x10\xfa\xd8\xae\x34\xc0\x1d\x9a\x39\x25\x61\x7b\x53\xa1\xb0\x9b\x15\xd9\x97\x85\x65\xf8\xc2\x25\x51\xd2\xe2\x4a\x82\x76\x2d\x81\x6d\x85\xa2\x10\x4c\xd9\xcb\x53\xf9\x0a\xfe\x19\x9f\xa0\x52\x52\x0b\x4a\x7a\x91\x0f\x6f\x33\x3e\xc9\x33\xbb\x44\xe2\x4e\xe2\xd1\x50\x2c\x27\xf4\x83\xc0\xdb\x16\x97\xd3\x6c\xfa\x44\xb5\xd0\x5f\x9b\xe7\xc6\x80\xe6\x73\x4a\x62\x1f\xd0\x45\x0a\x21\x06\xa6\xe8\x60\x98\xa6\x86\x92\x50\x21\x67\xea\x12\xad\xa3\x49\x94\x44\x61\x29\xd8\x58\x95\xc4\xf1\xa0\xdd\xd9\x1a\xb4\x7b\x71\x3b\xee\x77\x69\xaf\xd2\xa5\x30\x93\x1c\xbc\xa5\x4c\x4f\x40\x50\x12\x8e\xa2\xd6\xe0\xfc\x75\x27\xba\xf6\x46\x72\x0a\x72\x19\xc4\xbd\xeb\xba\x42\x76\x89\x1a\x3c\x8b\xbb\x91\xc7\x49\x45\x57\x59\xdc\xe9\x6e\xf5\xfe\xea\x27\x8b\x4c\xc5\x5c\x45\x6f\xbf\x7e\x1e\x8e\x86\xad\xff\xcb\xec\xda\xfb\x01\x2f\xd3\x2b\x62\xd0\x21\x47\xf5\x1f\x18\x2b\x8b\x29\x87\x9d\x28\x1e\x78\xe6\x0c\xd3\xf6\xb8\x10\x50\x12\x9e\xed\x1c\xfb\xe8\x04\x20\x63\x4a\xce\x80\x92\x4e\x44\x36\xc8\xed\xcf\x0f\xf7\x5f\xbe\xdd\xbd\xfd\x74\xff\xfe\xe3\xaf\xef\x6f\xee\x6e\xde\xdd\xfe\xb8\xb1\xb5\xf9\x4c\x6a\x0e\x87\xab\xad\xa8\x0e\xc5\xa5\x9b\xaf\x92\xad\xe5\xe3\xee\xa0\x42\x53\xd3\xf2\xc4\x45\xb7\xae\xff\xb4\xf9\x00\xd0\x77\xba\x98\x97\x93\xa8\x4f\x20\x43\xe3\x0e\xb5\x03\x33\x63\x8a\x92\x38\x2a\xb1\xa8\x75\x84\x30\xce\x4f\x1d\x73\xb9\xf5\xbe\xd1\x55\x1c\x56\xa4\xf6\x68\xc2\xcc\x49\x97\xa7\x70\x36\xcf\xe0\x89\x5c\x2d\x9a\x29\x66\x60\x98\x93\x5a\x34\x58\x0b\xc0\x3d\xcd\xcd\x3c\x5b\x57\x1e\x59\xda\x80\xf6\x73\x50\xa7\x73\xeb\x60\xda\x20\x18\x2a\x07\x46\x33\x87\x66\xad\x6c\x17\xd1\x1c\x21\x9f\x40\x53\xa1\xb1\x41\xed\x0a\x55\x03\x9f\xca\x74\x0d\xbd\x60\x7c\xb2\x06\xa7\x46\xce\xc0\xac\x11\xf0\xdc\x3a\x9c\xae\x11\x88\xcc\x1e\xad\x9f\xe1\x05\xc8\x14\xf3\x67\x44\x42\xa1\x66\xf6\x39\x2b\xc1\x94\x54\x80\xcf\xa8\x8a\x4b\x35\xcd\xbd\x7e\x6f\xf4\xc1\x03\x5b\xac\x43\x72\x5e\xee\xc4\xd5\x20\x7a\xf4\xcc\x56\x97\xe9\x2a\x1e\x45\xad\x7e\xa5\x8e\x93\x85\x9c\x29\xf7\x48\x5d\x2f\x58\xd9\xee\xba\xb4\xcd\x00\x9e\x5c\x50\x42\xd2\xf2\x9f\x5e\xae\xd6\x4a\xc9\x51\xdc\xea\x9c\x2f\xbe\xba\xa3\xa8\xd5\xf3\x9f\x61\x10\x54\xa9\x41\xf0\x3b\x00\x00\xff\xff\x4b\xc8\x57\x6f\x6d\x06\x00\x00")

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

	info := bindataFileInfo{name: "test/client/configs/default.yaml", size: 1645, mode: os.FileMode(420), modTime: time.Unix(1679041374, 0)}
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
