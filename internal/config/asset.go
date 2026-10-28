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

var _configsDefaultYaml = []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x02\xff\x6d\x91\xc1\x4e\xc3\x30\x10\x44\xef\xf9\x8a\x95\xef\x4d\xd3\x22\x44\xe4\x5b\xab\x16\x24\x04\xa2\x2a\xf0\x01\x6e\xd8\xb8\xa6\x8e\x1d\x79\x37\x55\xe1\xeb\xb1\x71\x11\x41\xe5\xe8\x37\xde\x19\xef\xd8\x7a\x2d\x0b\x80\xc6\x3b\xf2\x16\xd7\x4e\xed\x2c\x4a\xe0\x30\x60\xa4\xad\xb9\x40\x7d\x30\x8e\x17\x74\x4f\xde\x49\x68\x95\xa5\x04\xad\xd7\x0f\x78\x44\x2b\x41\xac\xd6\xcb\xd7\x3b\x91\xd9\xca\x04\x6c\xd8\x87\x8f\xc8\xcb\x69\x04\x34\x3d\x2b\xb7\x26\x59\x8a\x77\xae\xab\x7a\x42\x18\x8e\x18\x26\xda\x97\x51\x49\x17\x3a\x75\x7a\x36\x9f\xf8\xd4\x6e\xbd\xb5\xc6\x69\x09\xd7\x55\xc6\x4b\xd5\x1c\x86\x9e\x46\xca\x6c\x5e\x67\x69\xa1\xc7\x03\x37\x45\x91\x6d\xd3\x72\x4e\x75\xff\xa4\xa5\xa4\x68\xf7\x62\x3a\xf4\x03\x3f\x52\x4a\xa9\xce\x39\x5b\xe4\xa0\x1c\x75\x86\xc8\xc4\x66\x24\x5c\xa5\xd5\x7d\xe0\x64\x07\xc0\x4d\xbf\x49\x07\x10\xd1\xb1\x12\xdf\x6c\x78\x1b\xb1\x59\x66\x7b\xe6\x5f\x58\xd5\x09\xee\x94\x73\xf9\x55\x00\xf8\xb7\xda\x1f\x71\xa3\x78\x1f\x27\xe2\x97\xb4\x26\x56\x96\x61\xc9\x27\x16\xc5\x17\x36\x76\x1e\x0f\xae\x01\x00\x00")

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

	info := bindataFileInfo{name: "configs/default.yaml", size: 430, mode: os.FileMode(420), modTime: time.Unix(1678936086, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _testClientConfigsDefaultYaml = []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x02\xff\x85\x55\x3d\x6f\xdb\x30\x10\xdd\xf3\x2b\x04\x75\xb6\x23\xda\x71\x6c\x6b\x73\x3e\x91\x22\x45\x03\x27\xed\xd0\x20\x03\x43\x9d\x15\x36\x14\x29\x90\x94\x51\xb7\xce\x3f\x68\x86\x0e\xdd\x5a\x74\xed\xd0\xb1\x53\x3f\xd0\x3f\x93\x64\xed\x5f\xe8\x49\x94\x5c\x3b\x89\x6c\x40\x83\xf4\xde\xbb\x7b\xc7\xe3\x1d\x24\x54\x1c\xae\x79\x1e\x53\xd2\x28\x01\xbb\x92\x9e\x0b\x08\x3d\xab\x33\x40\x74\xc4\x1f\x40\xa9\xe6\xd2\x0e\xcc\x53\xa3\x64\xe8\x8d\xa8\x30\x39\x28\x54\x7c\x08\x63\x10\xa1\xe7\xef\xec\x6e\xbd\xd8\xf7\x1d\xb6\xc3\x35\x30\xab\xf4\x04\xf1\xe6\x3a\x02\x66\xbd\x64\xf6\x78\x9e\xd2\x7f\x6d\x7b\x41\xaf\xc1\x04\x07\x69\x1b\xb1\x6a\x22\x93\x0b\x12\xfa\xe6\x98\xbf\x85\xe7\xa3\xa1\x12\x82\xcb\x38\xf4\x3a\x81\x83\xb7\x28\xbb\xcc\x52\x33\xc7\x90\x56\xcf\x51\x83\x78\x3e\xa0\xbb\xe6\xb2\xe6\x67\x93\x34\x79\xc4\xcc\x77\xa7\x66\x99\xd6\x20\x19\x96\x48\x10\xc0\xf4\x27\x3c\x01\x95\xd9\x67\x26\x77\x0d\x4a\xdf\x21\x58\x4d\xa5\x49\xb8\x31\x1c\x3b\x15\x7a\x6d\x17\x2d\xf3\xfc\x9e\xa7\x21\x51\x16\x06\x51\xa4\xd1\x47\x28\x46\xc5\x85\x32\x36\x44\xbf\xc0\x2f\x04\x4f\x16\x25\x84\xf4\x9b\xad\x8d\x7e\x13\x1f\x42\x82\xb0\xb3\x4a\xd7\x21\x4d\xb2\xb9\x39\xd3\x45\x30\xe6\x0c\x9c\x35\x8f\x86\x80\xe7\xf5\x4f\x83\x46\xff\xec\x5d\xf7\xca\xe5\xc1\x33\xf0\x79\x9c\x74\xae\x2a\x83\xf4\x42\x49\x70\x1c\x69\x07\x8e\xee\x95\xec\x22\x47\x5a\xed\x8d\xce\x66\xb7\x37\x8b\x14\xd4\x96\xec\xcd\x8f\x6f\x83\xd3\x41\xe3\x55\x11\x5d\xe5\xbe\xc7\x17\xe1\x25\xa3\x95\x55\x4c\x89\x97\xa0\xf3\xfe\xa1\xa0\x15\x90\xbe\xe3\x8a\xc6\x1e\xe5\x02\x84\x4f\xb6\x8f\x1c\x7a\x09\x90\x52\xc1\xc7\x78\x71\xad\x00\xbd\x6f\xfe\x7c\xbe\xfb\xfe\xf3\xf6\xc3\xd7\xbb\x4f\x5f\xfe\xfe\x7a\x7f\x7b\xfd\xf1\xe6\xf7\xb5\xa9\x92\x8f\xb9\x64\x70\xb0\xd0\x89\x56\x59\x14\xe3\x76\xb2\xc8\x6c\xcc\x97\xbb\xad\x84\xd2\x15\x5b\x54\x9c\x77\xeb\xea\x7f\x97\xf7\x41\xb9\x46\xe7\xd7\x6a\xb1\xf8\x21\xa4\x4a\xdb\x03\x69\x41\x8f\x29\x4e\x3c\x09\x0a\x3a\xae\x74\xf9\x10\xb1\x63\x4b\x6d\x66\x5c\xde\x60\x4a\xfc\x92\xa9\x72\xd4\xd1\x48\xda\x2c\x82\x93\x49\x0a\x8f\xc4\xca\xb8\x9e\x55\x29\x68\x8c\x96\x71\x4d\x6a\x2c\x6f\x57\x32\x3d\x49\x97\xd9\x2b\x1a\xd5\x50\x7b\x19\x88\xe3\x89\xb1\x90\xd4\x08\x06\x02\xfb\x21\x29\x2e\xfb\x52\xd9\x8e\x52\xfa\x50\xb1\x4b\xa8\x33\x1a\x69\x25\x6d\xae\xaa\xe1\x13\x1e\x2d\x61\xcf\x71\x7f\x97\xd0\x91\xc6\x81\xd2\x4b\x04\x2c\x33\x56\x25\x4b\x04\x71\x6a\x0e\x97\xdf\xe1\x39\xf0\x48\x65\x2b\x44\x31\x5e\x26\x35\xab\x52\xc5\xb8\x00\x02\xd4\x0a\x55\x7e\xa8\xba\x7b\xaf\xe6\x2d\xbc\x37\x60\xb3\x75\xe8\x9d\x15\x3b\x31\xed\x07\x0f\xc6\x6c\x71\x99\xa6\x04\xdf\xba\xa5\x9a\xf4\x66\x72\x2a\xec\x03\x75\xb5\x60\x45\xbb\x2b\x6b\x93\x02\x3c\xba\xa0\xa8\x2b\x7e\x13\xc5\x6a\x2d\x58\x9e\x92\x46\xeb\x6c\xf6\xd5\xc6\xb7\x8e\xfb\xf4\xd7\xfe\x01\x7c\x5b\xc8\xa5\xb6\x06\x00\x00")

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

	info := bindataFileInfo{name: "test/client/configs/default.yaml", size: 1718, mode: os.FileMode(420), modTime: time.Unix(1688644570, 0)}
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
