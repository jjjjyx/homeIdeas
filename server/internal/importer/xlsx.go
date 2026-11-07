package importer

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

// 微信导出的账单是 .xlsx（支付宝是 .csv），这里用标准库直接读 OOXML，
// 不引入 excelize 这类大库：账单表格结构很规整，只需要共享字符串 + 单元格值。

type xlsxSheet struct {
	Rows []xlsxRow `xml:"sheetData>row"`
}

type xlsxRow struct {
	R     int        `xml:"r,attr"`
	Cells []xlsxCell `xml:"c"`
}

type xlsxCell struct {
	R  string   `xml:"r,attr"`
	T  string   `xml:"t,attr"`
	V  string   `xml:"v"`
	IS xlsxText `xml:"is"`
}

type xlsxText struct {
	T string    `xml:"t"`
	R []xlsxRun `xml:"r"`
}

type xlsxRun struct {
	T string `xml:"t"`
}

type xlsxSST struct {
	SI []xlsxText `xml:"si"`
}

// readXLSXRows 把 sheet 读成二维字符串表。
func readXLSXRows(data []byte) ([][]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("不是有效的 xlsx 文件: %w", err)
	}

	var sheetName string
	var sharedData []byte
	sheetFiles := map[string][]byte{}

	for _, f := range zr.File {
		name := f.Name
		switch {
		case name == "xl/sharedStrings.xml":
			sharedData, err = readZipEntry(f)
			if err != nil {
				return nil, err
			}
		case strings.HasPrefix(name, "xl/worksheets/") && strings.HasSuffix(name, ".xml"):
			content, err := readZipEntry(f)
			if err != nil {
				return nil, err
			}
			sheetFiles[name] = content
			// 默认取 sheet1，找不到就用任意一个
			if sheetName == "" || strings.HasSuffix(name, "sheet1.xml") {
				sheetName = name
			}
		}
	}
	if sheetName == "" {
		return nil, fmt.Errorf("xlsx 里没有工作表")
	}

	var shared []string
	if len(sharedData) > 0 {
		var sst xlsxSST
		if err := xml.Unmarshal(sharedData, &sst); err == nil {
			shared = make([]string, 0, len(sst.SI))
			for _, si := range sst.SI {
				shared = append(shared, flattenText(si))
			}
		}
	}

	var sheet xlsxSheet
	if err := xml.Unmarshal(sheetFiles[sheetName], &sheet); err != nil {
		return nil, fmt.Errorf("解析工作表失败: %w", err)
	}

	out := make([][]string, 0, len(sheet.Rows))
	for _, row := range sheet.Rows {
		cells := map[int]string{}
		maxCol := -1
		for _, c := range row.Cells {
			col := columnIndex(c.R)
			if col < 0 {
				continue
			}
			cells[col] = cellValue(c, shared)
			if col > maxCol {
				maxCol = col
			}
		}
		line := make([]string, maxCol+1)
		for i := 0; i <= maxCol; i++ {
			line[i] = strings.TrimSpace(cells[i])
		}
		out = append(out, line)
	}
	return out, nil
}

func readZipEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, 32<<20))
}

func flattenText(t xlsxText) string {
	if t.T != "" {
		return t.T
	}
	var sb strings.Builder
	for _, r := range t.R {
		sb.WriteString(r.T)
	}
	return sb.String()
}

func cellValue(c xlsxCell, shared []string) string {
	switch c.T {
	case "s":
		idx, err := strconv.Atoi(strings.TrimSpace(c.V))
		if err == nil && idx >= 0 && idx < len(shared) {
			return shared[idx]
		}
		return ""
	case "inlineStr":
		return flattenText(c.IS)
	case "str":
		return c.V
	default:
		return c.V
	}
}

// columnIndex 把 "A1"/"AB12" 里的列字母转成从 0 开始的下标
func columnIndex(ref string) int {
	idx := 0
	seen := false
	for _, ch := range ref {
		if ch >= 'A' && ch <= 'Z' {
			idx = idx*26 + int(ch-'A'+1)
			seen = true
			continue
		}
		if ch >= 'a' && ch <= 'z' {
			idx = idx*26 + int(ch-'a'+1)
			seen = true
			continue
		}
		break
	}
	if !seen {
		return -1
	}
	return idx - 1
}

// ParseXLSX 解析 xlsx 账单（微信导出的是这种）。
func ParseXLSX(data []byte, fileName string) (*Result, error) {
	rows, err := readXLSXRows(data)
	if err != nil {
		return nil, err
	}
	// 用前几十行拼一段文本用于来源识别
	var sb strings.Builder
	for i, row := range rows {
		if i > 40 {
			break
		}
		sb.WriteString(strings.Join(row, " "))
		sb.WriteString("\n")
	}
	src := detectSource(sb.String(), fileName)
	return parseRows(rows, src)
}

// IsXLSX 判断字节流是不是 xlsx（本质是 zip，里面必须有 xl/workbook.xml）
func IsXLSX(data []byte) bool {
	if !IsZip(data) {
		return false
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return false
	}
	for _, f := range zr.File {
		if f.Name == "xl/workbook.xml" {
			return true
		}
	}
	return false
}

func isXLSXName(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".xlsx", ".xlsm":
		return true
	}
	return false
}
