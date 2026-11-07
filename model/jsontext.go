package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
)

// JSONText 把 jsonb 列映射为可直接放进 JSON 响应的对象。
//
// 空字符串在写入时转成 NULL，在序列化时转成 null；非空值必须是合法 JSON。
type JSONText string

// Value 实现 driver.Valuer。
func (j JSONText) Value() (driver.Value, error) {
	s := strings.TrimSpace(string(j))
	if s == "" {
		return nil, nil
	}
	if !json.Valid([]byte(s)) {
		return nil, fmt.Errorf("invalid json")
	}
	return s, nil
}

// Scan 实现 sql.Scanner。
func (j *JSONText) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		*j = ""
		return nil
	case []byte:
		*j = JSONText(v)
		return nil
	case string:
		*j = JSONText(v)
		return nil
	default:
		return fmt.Errorf("cannot scan %T into JSONText", value)
	}
}

// MarshalJSON 把存储的 JSON 原样输出；空值输出 null。
func (j JSONText) MarshalJSON() ([]byte, error) {
	s := strings.TrimSpace(string(j))
	if s == "" {
		return []byte("null"), nil
	}
	return []byte(s), nil
}

// UnmarshalJSON 保留原始 JSON。
func (j *JSONText) UnmarshalJSON(data []byte) error {
	*j = JSONText(data)
	return nil
}
