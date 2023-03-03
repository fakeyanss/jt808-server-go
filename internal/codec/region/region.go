package region

import (
	gb2260 "github.com/cn/GB2260.go"
)

const CodeNotFound = "NotFound"

var gb = gb2260.NewGB2260("")

type AdministrativeRegion struct {
	Code string `json:"code"` // The six-digit number of the specific administrative division.
	Name string `json:"name"` // The Chinese name of the specific administrative division.
}

// todo: name解析为xx省[xx市][xx区]

func Parse(code string) *AdministrativeRegion {
	division := gb.Get(code)
	ar := &AdministrativeRegion{Code: code}
	if division == nil {
		ar.Name = CodeNotFound
	} else {
		ar.Name = division.Name
	}
	return ar
}
