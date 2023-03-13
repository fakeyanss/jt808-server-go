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

	info := bindataFileInfo{name: "configs/default.yaml", size: 385, mode: os.FileMode(420), modTime: time.Unix(1678709614, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _testClientConfigsDefaultYaml = []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x00\xff\x84\x94\x3d\x6f\xdb\x3c\x10\xc7\x77\x7d\x0a\x42\xcf\xac\x44\x72\x62\x44\xe1\xe6\xbc\x22\x0f\x02\x3c\x41\x92\xa7\x43\x8d\x0c\x0c\x75\x66\x58\x53\x3c\x81\xa4\x8c\xba\x4d\xbe\x41\x33\x74\xe8\xd6\xa2\x6b\x87\x8e\x9d\xfa\x82\x7e\x99\x24\x6b\xbf\x42\x21\x53\x72\xed\xc6\x52\x46\xde\xef\x7f\xff\xd3\x1d\x79\x52\x28\x68\x40\x08\x47\x6d\x51\xc1\xbe\x66\x97\x0a\x28\x71\xa6\x84\x80\x90\x91\x7c\x14\x2a\x8c\xd4\x6e\x60\xff\xb5\xa8\x29\x19\x31\x65\xab\xa0\x42\x71\x0c\x13\x50\x94\x84\x7b\xfb\x3b\xff\x1f\x86\x3e\xb6\x27\x0d\x70\x87\x66\x4a\x49\xb8\xb6\xae\x50\xd8\xf5\x9a\x1c\xc8\xca\x32\x7c\xe1\xd2\x38\x8d\xb8\x92\xa0\x5d\x24\x70\x4d\xa1\xa8\x04\x39\x7b\x79\x26\x5f\xc1\x7f\xa3\x53\x54\x4a\x6a\x41\x49\x3f\xf6\xe1\x1d\xc6\xc7\x65\x61\x17\x48\xd2\x4b\x3d\x1a\x88\xc5\x84\xad\x20\xf0\xb6\x55\x73\x9a\xe5\x2b\xaa\x85\xbe\x6d\x5e\x1a\x03\x9a\x4f\x29\x49\x62\x1f\xd1\x55\x0e\x21\x06\x72\x74\x30\xc8\x32\x43\x49\xa8\x90\x33\x75\x85\xd6\xd1\x34\x4e\xe3\x2a\x35\x83\x89\xe4\xe0\xa5\x32\x3b\x05\x41\x49\x38\x8c\xa3\xed\x8b\xd7\xbd\xf8\x26\xf4\xe1\x1c\xe4\x22\x48\xfa\x35\x28\xae\x50\xc3\xca\x94\x42\x31\x57\x93\xbb\xaf\x9f\x07\xc3\x41\xf4\x7c\x26\x98\x67\x1a\x74\xc8\x51\x3d\x03\x63\x65\x75\x05\x61\x2f\x4e\xb6\x3d\x73\x86\x69\x7b\x52\x09\x28\x09\xcf\x77\x4f\x7c\x74\x0c\x50\x30\x25\x27\x40\x49\x2f\x26\xff\x90\xbb\x9f\x1f\x1e\xbe\x7c\xbb\x7f\xfb\xe9\xe1\xfd\xc7\x5f\xdf\xdf\xdc\xdf\xbe\xbb\xfb\x71\x6b\x1b\xf3\x89\xd4\x1c\x8e\x96\xdb\xa9\x4b\x73\xe9\xa6\xcb\x64\x73\xf1\xa3\x77\x51\xa1\x69\x68\xd2\xdb\xd8\xec\x57\x1d\xdf\xfc\x19\xd5\x21\xa0\x9f\x56\x35\x4b\x27\x51\x9f\x42\x81\xc6\x1d\x69\x07\x66\xc2\x54\x3d\x7f\x42\x44\xa3\x23\x84\x71\x7e\xe6\x98\x2b\xad\xf7\x8d\xaf\x93\xb0\x26\x8d\x47\x1b\x66\x4e\xba\x32\x83\xf3\x69\x01\x2b\x72\xb5\x68\xa7\x58\x80\x61\x4e\x6a\xd1\x62\x2d\x00\xf7\x35\x37\xd3\xa2\xab\x3c\xb2\xac\x05\x1d\x94\xa0\xce\xa6\xd6\x41\xde\x22\x18\x28\x07\x46\x33\x87\xa6\x53\xb6\x87\x68\x8e\x91\x8f\xa1\xad\xd0\xc8\xa0\x76\x95\xaa\x85\xe7\x32\xeb\xa0\x97\x8c\x8f\x3b\x70\x66\xe4\x04\x4c\x87\x80\x97\xd6\x61\xde\x21\x10\x85\x3d\xee\xbe\xc3\x4b\x90\x19\x96\x4f\x88\x84\x42\xcd\xec\x53\x56\x82\x29\xa9\x00\x9f\x50\x55\x4d\xb5\xdd\x7b\xf3\xde\xe8\x5f\x0f\x6c\xbe\x0e\xe9\xc5\x6c\x27\xae\xb7\xe3\x47\xcf\x6c\x79\x99\xae\x93\x61\x1c\x6d\xd5\xea\x24\x9d\xcb\x99\x72\x8f\xd4\xcd\x82\xcd\xc6\xdd\x94\xb6\x05\xc0\xca\x05\x25\x24\x9b\xfd\x70\x67\xab\xb5\x54\x72\x98\x44\xbd\x8b\xf9\x69\x63\x18\x47\x7d\x7f\x0c\x83\xa0\x4e\x0d\x82\xdf\x01\x00\x00\xff\xff\x21\x77\xdf\xd8\x0a\x06\x00\x00")

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

	info := bindataFileInfo{name: "test/client/configs/default.yaml", size: 1546, mode: os.FileMode(420), modTime: time.Unix(1678688453, 0)}
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
