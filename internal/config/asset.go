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

	info := bindataFileInfo{name: "configs/banner.txt", size: 509, mode: os.FileMode(420), modTime: time.Unix(1678892343, 0)}
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

	info := bindataFileInfo{name: "configs/default.yaml", size: 385, mode: os.FileMode(420), modTime: time.Unix(1678892343, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _testClientConfigsDefaultYaml = []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\xff\x84\x94\xbd\x6e\xdb\x30\x10\xc7\x77\x3d\x05\xa1\xce\x4e\x28\x27\x46\x14\x6e\xce\x27\x52\x04\x68\x90\xa4\x1d\x6a\x64\x60\xa8\x33\xc3\x9a\xe6\x09\x24\x65\xd4\x6d\xf2\x06\xcd\xd0\xa1\x5b\x8b\xae\x1d\x3a\x76\xea\x07\xfa\x32\x49\xd6\xbe\x42\x21\x53\x72\xed\x26\x52\x46\xde\xef\x7f\xff\xd3\x1d\x79\xd2\x28\x59\x44\x88\x40\xe3\x50\xc3\xae\xe1\xe7\x1a\x18\xf1\xb6\x80\x88\x90\xa1\xba\x17\xca\xad\x32\xbe\xef\x9e\x3a\x34\x8c\x0c\xb9\x76\x65\x50\xa3\x3c\x84\x09\x68\x46\xe2\x9d\xdd\xad\xe7\xfb\x71\x88\xed\x28\x0b\xc2\xa3\x9d\x32\x12\xaf\xac\x6a\x94\x6e\xb5\x22\x7b\xaa\xb4\x8c\x5f\xf9\x94\xa6\x1d\xa1\x15\x18\xdf\x91\xb8\xa2\x51\x96\x82\x31\x7f\x7d\xa2\xde\xc0\xb3\xe1\x31\x6a\xad\x8c\x64\xa4\x47\x43\x78\x8b\x8b\x51\x91\xbb\x05\x92\x74\xd3\x80\xfa\x72\x31\x61\x23\x8a\x82\x6d\xd9\x9c\xe1\xe3\x07\xaa\xc5\xa1\x6d\x51\x58\x0b\x46\x4c\x19\x49\x42\xc0\x94\x29\x84\x58\x18\xa3\x87\x7e\x96\x59\x46\x62\x8d\x82\xeb\x0b\x74\x9e\xa5\x34\xa5\x65\x66\x06\x13\x25\x20\x48\x55\x76\x0c\x92\x91\x78\x40\x3b\x9b\x67\x6f\xbb\xf4\x2a\x0e\xe1\x31\xa8\x45\x90\xf4\x2a\x90\x5f\xa0\x81\x40\x92\x35\x1a\x60\x5a\x33\xcd\x7d\xc5\x6e\xbe\x7f\xed\x0f\xfa\x9d\x97\x33\x3e\xcf\xb5\xe8\x51\xa0\x7e\x01\xd6\xa9\xf2\x0e\xe2\x2e\x4d\x36\x03\xf3\x96\x1b\x77\x54\x0a\x18\x89\x4f\xb7\x8f\x42\x74\x04\x90\x73\xad\x26\xc0\x48\x97\x92\x27\xe4\xe6\xf7\xa7\xbb\x6f\x3f\x6e\xdf\x7f\xb9\xfb\xf8\xf9\xcf\xcf\x77\xb7\xd7\x1f\x6e\x7e\x5d\xbb\xda\x7c\xa2\x8c\x80\x83\xe5\x86\xaa\xd2\x42\xf9\xe9\x32\x59\x5f\xfc\xe8\x6d\xd4\x68\x6b\x9a\x74\xd7\xd6\x7b\x65\xcf\x57\xff\x86\xb5\x0f\x18\xe6\x55\x4e\xd3\x2b\x34\xc7\x90\xa3\xf5\x07\xc6\x83\x9d\x70\xcd\x48\x42\x67\x58\xd6\x3a\x42\xb8\x10\x27\x9e\xfb\xc2\x05\x5f\x7a\x99\xc4\x15\xa9\x3d\x9a\x30\xf7\xca\x17\x19\x9c\x4e\x73\x78\x20\xd7\xc8\x66\x8a\x39\x58\xee\x95\x91\x0d\xd6\x12\x70\xd7\x08\x3b\xcd\xdb\xca\x23\xcf\x1a\xd0\x5e\x01\xfa\x64\xea\x3c\x8c\x1b\x04\x7d\xed\xc1\x1a\xee\xd1\xb6\xca\x76\x10\xed\x21\x8a\x11\x34\x15\x1a\x5a\x34\xbe\x54\x35\xf0\xb1\xca\x5a\xe8\x39\x17\xa3\x16\x9c\x59\x35\x01\xdb\x22\x10\x85\xf3\x38\x6e\x11\xc8\xdc\x1d\xb6\xdf\xe1\x39\xa8\x0c\x8b\x47\x44\x52\xa3\xe1\xee\x31\x2b\xc9\xb5\xd2\x80\x8f\xa8\xca\xa6\x9a\xee\xbd\x7e\x6f\xec\xbf\x07\x36\x5f\x87\xf4\x6c\xb6\x13\x97\x9b\xf4\xde\x33\x5b\x5e\xa6\xcb\x64\x40\x3b\x1b\x95\x3a\x49\xe7\x72\xae\xfd\x3d\x75\xbd\x60\xb3\x71\xd7\xa5\x5d\x0e\xf0\xe0\x82\x12\x92\xcd\xfe\xb8\xb3\xd5\x5a\x2a\x39\x48\x3a\xdd\xb3\xf9\x69\x6d\x40\x3b\xbd\x70\x8c\xa3\xa8\x4a\x8d\xa2\xbf\x01\x00\x00\xff\xff\xf1\x05\x8d\x83\x0b\x06\x00\x00")

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

	info := bindataFileInfo{name: "test/client/configs/default.yaml", size: 1547, mode: os.FileMode(420), modTime: time.Unix(1678892343, 0)}
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
