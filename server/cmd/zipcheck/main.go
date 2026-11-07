package main

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"flag"
	"fmt"
	"hash/crc32"
	"io"
	"log"
	"os"

	"github.com/yeka/zip"
)

// 独立实现的 ZipCrypto（PKZIP 传统加密）校验/解密工具。
//
// 用途：判断「解压失败」到底是密码错，还是 zip 库的问题。
// ZipCrypto 的 12 字节加密头最后一位等于 DOS 时间的高字节（设置了 data descriptor 时）
// 或 CRC 的高字节，可以据此快速判断密码是否正确。
//
//	go run ./cmd/zipcheck -file bill.zip -pass 123456
func main() {
	file := flag.String("file", "", "zip 文件路径")
	password := flag.String("pass", "", "解压密码")
	out := flag.String("out", "", "解密成功后把内容写到这里（可选）")
	selfTest := flag.Bool("selftest", false, "用自造的加密包验证本工具是否正确")
	flag.Parse()

	if *selfTest {
		runSelfTest()
		return
	}

	data, err := os.ReadFile(*file)
	if err != nil {
		log.Fatalf("读取失败: %v", err)
	}

	h, err := parseLocalHeader(data)
	if err != nil {
		log.Fatalf("解析 zip 头失败: %v", err)
	}
	fmt.Printf("文件: %s (%d 字节)\n", *file, len(data))
	fmt.Printf("  method=%d flag=0x%04X crc=0x%08X csize=%d uszie=%d\n",
		h.method, h.flags, h.crc, h.compressedSize, h.uncompressedSize)
	fmt.Printf("  dosTime=0x%04X name=%q\n", h.dosTime, h.name)
	fmt.Printf("  encrypted=%v dataDescriptor=%v\n",
		h.flags&1 == 1, h.flags&8 == 8)

	if h.flags&1 == 0 {
		log.Fatalf("这个 zip 没有加密，不需要密码")
	}

	keys := initKeys([]byte(*password))
	header := make([]byte, 12)
	copy(header, data[h.dataOffset:h.dataOffset+12])
	decHeader := make([]byte, 12)
	for i, b := range header {
		decHeader[i] = decryptByte(&keys, b)
	}

	// 设置了 data descriptor 时用 DOS 时间高字节，否则用 CRC 高字节
	var want byte
	if h.flags&8 == 8 {
		want = byte(h.dosTime >> 8)
	} else {
		want = byte(h.crc >> 24)
	}
	fmt.Printf("\n密码校验: 期望 0x%02X，实际 0x%02X -> %s\n",
		want, decHeader[11], map[bool]string{true: "✓ 密码正确", false: "✗ 密码错误"}[want == decHeader[11]])

	if want != decHeader[11] {
		os.Exit(1)
	}

	// 密码对了，继续解压验证
	keys = initKeys([]byte(*password))
	for _, b := range header {
		_ = decryptByte(&keys, b)
	}
	compressed := make([]byte, h.compressedSize-12)
	for i := range compressed {
		compressed[i] = decryptByte(&keys, data[h.dataOffset+12+i])
	}

	r := flate.NewReader(bytes.NewReader(compressed))
	plain, err := io.ReadAll(r)
	if err != nil {
		log.Fatalf("解压失败: %v", err)
	}
	fmt.Printf("解压成功，%d 字节\n", len(plain))
	if *out != "" {
		if err := os.WriteFile(*out, plain, 0o600); err != nil {
			log.Fatalf("写入失败: %v", err)
		}
		fmt.Printf("已写入 %s\n", *out)
	}
}

type localHeader struct {
	method           uint16
	flags            uint16
	crc              uint32
	compressedSize   int
	uncompressedSize int
	dosTime          uint16
	name             string
	dataOffset       int
}

func parseLocalHeader(data []byte) (*localHeader, error) {
	if len(data) < 30 || string(data[:4]) != "PK\x03\x04" {
		return nil, fmt.Errorf("不是 zip 文件")
	}
	h := &localHeader{
		flags:   binary.LittleEndian.Uint16(data[6:8]),
		method:  binary.LittleEndian.Uint16(data[8:10]),
		dosTime: binary.LittleEndian.Uint16(data[10:12]),
		crc:     binary.LittleEndian.Uint32(data[14:18]),
	}
	nameLen := int(binary.LittleEndian.Uint16(data[26:28]))
	extraLen := int(binary.LittleEndian.Uint16(data[28:30]))
	h.name = string(data[30 : 30+nameLen])
	h.dataOffset = 30 + nameLen + extraLen

	h.compressedSize = int(binary.LittleEndian.Uint32(data[18:22]))
	h.uncompressedSize = int(binary.LittleEndian.Uint32(data[22:26]))

	// 设置了 data descriptor（bit 3）时，本地头里的 size 为 0，要去中央目录取
	if h.flags&8 == 8 || h.compressedSize == 0 {
		cs, us, crc := findCentralDirectory(data)
		if cs > 0 {
			h.compressedSize = cs
			h.uncompressedSize = us
			h.crc = crc
		}
	}
	if h.compressedSize == 0 {
		return nil, fmt.Errorf("无法确定压缩数据长度")
	}
	return h, nil
}

// findCentralDirectory 从 zip 尾部找中央目录，取第一个条目的 size/crc
func findCentralDirectory(data []byte) (compressed, uncompressed int, crc uint32) {
	for i := len(data) - 4; i >= 0; i-- {
		if data[i] == 'P' && data[i+1] == 'K' && data[i+2] == 1 && data[i+3] == 2 {
			if i+46 > len(data) {
				return
			}
			crc = binary.LittleEndian.Uint32(data[i+16 : i+20])
			compressed = int(binary.LittleEndian.Uint32(data[i+20 : i+24]))
			uncompressed = int(binary.LittleEndian.Uint32(data[i+24 : i+28]))
			return
		}
	}
	return
}

var crcTable = crc32.MakeTable(crc32.IEEE)

type keys struct{ k0, k1, k2 uint32 }

func initKeys(password []byte) keys {
	ks := keys{0x12345678, 0x23456789, 0x34567890}
	for _, b := range password {
		updateKeys(&ks, b)
	}
	return ks
}

// crcStep 是 PKZIP 用的裸 CRC 表步进。
// 注意：不能用 crc32.Update —— Go 的实现会在内部取反，
// 和 PKZIP 的 (crc>>8)^table[(crc^b)&0xff] 不等价，用它算出来的密钥全是错的。
func crcStep(crc uint32, b byte) uint32 {
	return (crc >> 8) ^ crcTable[byte(crc)^b]
}

func updateKeys(ks *keys, b byte) {
	ks.k0 = crcStep(ks.k0, b)
	ks.k1 = (ks.k1 + uint32(ks.k0&0xff)) * 134775813
	ks.k1++
	ks.k2 = crcStep(ks.k2, byte(ks.k1>>24))
}

func decryptByte(ks *keys, c byte) byte {
	t := ks.k2 | 2
	k := byte((t * (t ^ 1)) >> 8)
	p := c ^ k
	updateKeys(ks, p)
	return p
}

// runSelfTest 用 yeka/zip 造一个密码已知的 ZipCrypto 包，再用自己的实现去校验，
// 两边结论一致才说明这个校验工具是可信的。
func runSelfTest() {
	const password = "test123"
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Encrypt("selftest.csv", password, zip.StandardEncryption)
	if err != nil {
		log.Fatalf("创建测试包失败: %v", err)
	}
	_, _ = f.Write([]byte("hello"))
	_ = w.Close()
	data := buf.Bytes()

	h, err := parseLocalHeader(data)
	if err != nil {
		log.Fatalf("解析失败: %v", err)
	}
	check := func(pw string) bool {
		ks := initKeys([]byte(pw))
		dec := make([]byte, 12)
		for i, b := range data[h.dataOffset : h.dataOffset+12] {
			dec[i] = decryptByte(&ks, b)
		}
		var want byte
		if h.flags&8 == 8 {
			want = byte(h.dosTime >> 8)
		} else {
			want = byte(h.crc >> 24)
		}
		return dec[11] == want
	}

	// 诊断输出
	ks := initKeys([]byte(password))
	dec := make([]byte, 12)
	for i, b := range data[h.dataOffset : h.dataOffset+12] {
		dec[i] = decryptByte(&ks, b)
	}
	fmt.Printf("自检诊断: flags=0x%04X crc=0x%08X dosTime=0x%04X\n", h.flags, h.crc, h.dosTime)
	fmt.Printf("  解密头 12 字节: % X\n", dec)
	fmt.Printf("  crc>>24=0x%02X  dosTime>>8=0x%02X  dec[11]=0x%02X\n", byte(h.crc>>24), byte(h.dosTime>>8), dec[11])

	ok := check(password)
	bad := check("wrong-password")
	fmt.Printf("自检：正确密码 %v，错误密码 %v -> %s\n", ok, bad,
		map[bool]string{true: "✓ 工具可信", false: "✗ 工具有问题"}[ok && !bad])
	if !ok || bad {
		os.Exit(1)
	}
}
