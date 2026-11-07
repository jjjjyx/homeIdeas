package secretbox_test

import (
	"testing"

	"family-ledger/server/internal/pkg/secretbox"
)

func TestEncryptDecrypt(t *testing.T) {
	const key = "test-data-key-0123456789"
	plain := "imap-授权码-123456"

	enc, err := secretbox.Encrypt(key, plain)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	if enc == plain || enc == "" {
		t.Fatal("密文不应等于明文")
	}

	got, err := secretbox.Decrypt(key, enc)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if got != plain {
		t.Fatalf("解密结果不一致: %q != %q", got, plain)
	}

	// 换密钥必须解不开
	if _, err := secretbox.Decrypt("another-key", enc); err == nil {
		t.Fatal("错误密钥应该解密失败")
	}

	// 同一明文两次加密的密文不同（随机 nonce）
	enc2, _ := secretbox.Encrypt(key, plain)
	if enc == enc2 {
		t.Fatal("相同明文两次加密应产生不同密文")
	}
}

func TestEmptyValue(t *testing.T) {
	enc, err := secretbox.Encrypt("k", "")
	if err != nil || enc != "" {
		t.Fatalf("空串应返回空串: %q %v", enc, err)
	}
	got, err := secretbox.Decrypt("k", "")
	if err != nil || got != "" {
		t.Fatalf("空密文应返回空串: %q %v", got, err)
	}
}
