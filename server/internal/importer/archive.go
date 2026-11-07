package importer

import (
	"bytes"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/yeka/zip"
)

// IsZip 判断是不是 zip（微信/支付宝的账单都是加密 zip）
func IsZip(data []byte) bool {
	return len(data) > 4 && data[0] == 'P' && data[1] == 'K' && data[2] == 3 && data[3] == 4
}

// ExtractBillFile 从（可能是加密的）zip 里取出第一个账单文件。
// 微信导出的外层是 zip、里面是 .xlsx；支付宝里面是 .csv。
func ExtractBillFile(data []byte, password string) (name string, content []byte, err error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", nil, fmt.Errorf("不是有效的 zip 文件: %w", err)
	}
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		ext := strings.ToLower(path.Ext(f.Name))
		switch ext {
		case ".csv", ".txt", ".xlsx", ".xlsm":
		default:
			continue
		}
		// 注意：yeka/zip 在「文件已加密但没设置密码」时会直接 panic，
		// 所以必须显式判断，绝不能把空密码传给 SetPassword。
		if f.IsEncrypted() {
			if password == "" {
				return "", nil, fmt.Errorf("账单压缩包已加密，需要解压密码（微信/支付宝申请账单时会给出）")
			}
			f.SetPassword(password)
		}
		rc, err := f.Open()
		if err != nil {
			return "", nil, fmt.Errorf("打开压缩包内文件失败（解压密码可能不对）: %w", err)
		}
		buf, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return "", nil, fmt.Errorf("读取压缩包内文件失败（解压密码可能不对）: %w", err)
		}
		return f.Name, buf, nil
	}
	return "", nil, fmt.Errorf("压缩包里没有找到账单文件（.csv/.xlsx）")
}

// ParseAny 入口：zip 先解包，再按扩展名分派到 CSV 或 xlsx 解析器。
//
// 解析第三方文件，任何库 panic 都不能把调用方（尤其是后台邮件轮询）带崩，
// 这里兜底 recover 成普通错误。
func ParseAny(fileName string, data []byte, password string) (result *Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			result = nil
			err = fmt.Errorf("解析账单文件时出错: %v", r)
		}
	}()

	// 微信：外层是加密 zip，里面是 xlsx
	if IsZip(data) && !IsXLSX(data) {
		name, content, err := ExtractBillFile(data, password)
		if err != nil {
			return nil, err
		}
		if fileName == "" {
			fileName = name
		}
		data = content
	}

	if isXLSXName(fileName) || IsXLSX(data) {
		return ParseXLSX(data, fileName)
	}
	return ParseCSV(data, fileName)
}
