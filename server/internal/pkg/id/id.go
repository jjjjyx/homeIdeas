package id

import "github.com/google/uuid"

// New 生成 UUIDv7：时间有序，适合做主键和同步游标的次级排序键。
func New() string {
	if v, err := uuid.NewV7(); err == nil {
		return v.String()
	}
	return uuid.NewString()
}
