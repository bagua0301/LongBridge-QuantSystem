package utils

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"LongBridge-QuantSystem/types"

	"github.com/shopspring/decimal"
)

// Exists 判断所给路径文件/文件夹是否存在
func Exists(path string) bool {
	_, err := os.Stat(path)
	if err != nil {
		if os.IsExist(err) {
			return true
		}
		return false
	}
	return true
}

// GetExecpath 获取当前程序运行目录
func GetExecpath() string {
	execpath, _ := os.Executable()
	path := filepath.Dir(execpath)
	return strings.Replace(path, "\\", "/", -1)
}

// ============================================
// 数学辅助函数
// ============================================

// GetEnvInt 获取环境变量 int
func GetEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

// GetEnvInt32 获取环境变量 int32
func GetEnvInt32(key string, defaultValue int32) int32 {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.ParseInt(value, 10, 32); err == nil {
			return int32(intVal)
		}
	}
	return defaultValue
}

// GetEnvInt64 获取环境变量 int64
func GetEnvInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
			return intVal
		}
	}
	return defaultValue
}

// GetEnvUint 获取环境变量 uint
func GetEnvUint(key string, defaultValue uint) uint {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.ParseUint(value, 10, 0); err == nil {
			return uint(intVal)
		}
	}
	return defaultValue
}

// GetEnvUint32 获取环境变量 uint32
func GetEnvUint32(key string, defaultValue uint32) uint32 {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.ParseUint(value, 10, 32); err == nil {
			return uint32(intVal)
		}
	}
	return defaultValue
}

// GetEnvUint64 获取环境变量 uint64
func GetEnvUint64(key string, defaultValue uint64) uint64 {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.ParseUint(value, 10, 64); err == nil {
			return intVal
		}
	}
	return defaultValue
}

// GetEnvFloat32 获取环境变量 float32
func GetEnvFloat32(key string, defaultValue float32) float32 {
	if value := os.Getenv(key); value != "" {
		if floatVal, err := strconv.ParseFloat(value, 32); err == nil {
			return float32(floatVal)
		}
	}
	return defaultValue
}

// GetEnvFloat64 获取环境变量 float64
func GetEnvFloat64(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
			return floatVal
		}
	}
	return defaultValue
}

// GetEnvBool 获取环境变量 bool
func GetEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolVal, err := strconv.ParseBool(value); err == nil {
			return boolVal
		}
	}
	return defaultValue
}

func GetEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// GetEnvString 获取环境变量 string
func GetEnvString(key string, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func GetEnvSymbols(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		parts := strings.Split(value, ",")
		symbols := make([]string, 0, len(parts))
		for _, part := range parts {
			symbol := strings.TrimSpace(part)
			if symbol != "" {
				symbols = append(symbols, symbol)
			}
		}
		if len(symbols) > 0 {
			return symbols
		}
	}
	return defaultValue
}

// Min 返回两个浮点数中的最小值
func Min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// Max 返回两个浮点数中的最大值
func Max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func Abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Round 四舍五入到指定小数位数
func Round(value float64, precision int) float64 {
	multiplier := 1.0
	for i := 0; i < precision; i++ {
		multiplier *= 10
	}
	return float64(int(value*multiplier+0.5)) / multiplier
}

// RoundToUSStockPrice 四舍五入到美股最小价格变动单位（0.01）
func RoundToUSStockPrice(price float64) float64 {
	// 直接四舍五入到两位小数
	return math.Round(price*100) / 100
}

// Percentage 计算百分比
func Percentage(part, total float64) float64 {
	if total == 0 {
		return 0
	}
	return (part / total) * 100
}

// 安全地将 decimal.Decimal 转换为 float64
func DecimalToFloat64(d *decimal.Decimal) (float64, bool) {
	if d == nil {
		return 0, false
	}

	if f, ok := d.Float64(); ok {
		return f, true
	}

	priceStr := d.String()
	if priceStr == "" || priceStr == "null" {
		return 0, false
	}

	f, err := strconv.ParseFloat(priceStr, 64)
	if err != nil {
		return 0, false
	}

	return f, true
}

func StringToInt32(s string) (int32, error) {
	val, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return 0, err
	}
	return int32(val), nil
}

func StringToInt64(s string) (int64, error) {
	val, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return int64(val), nil
}

// 是否做空
func IsShortOrderCategory(category int32) bool {
	return types.OrderCategory(category) == types.ShortOrderCategory
}

// NormalizeFieldName 标准化字段名，支持多种输入格式
func NormalizeFieldName(name string) string {
	// 移除下划线，统一小写
	name = strings.ReplaceAll(name, "_", "")
	return strings.ToLower(name)
}

// ToSnakeCase 将驼峰命名转为下划线命名
func ToSnakeCase(s string) string {
	var result []rune
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result = append(result, '_')
		}
		result = append(result, r)
	}
	return strings.ToLower(string(result))
}

// FormatUnixTimestamp 格式化 Unix 时间戳
func FormatUnixTimestamp(timestamp int64) string {
	return time.Unix(timestamp, 0).Format("2006-01-02 15:04:05")
}

// FormatTime 格式化 time.Time
func FormatTime(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

// formatQuantity 格式化数量（整数不显示小数点）
func FormatQuantity(qty float64) string {
	if qty == float64(int64(qty)) {
		return fmt.Sprintf("%.2f", qty)
	}
	return fmt.Sprintf("%.2f", qty)
}

// TruncateString 截断字符串（支持中文）
func TruncateString(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}

	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}

	if maxLen <= 3 {
		return string(runes[:maxLen])
	}

	return string(runes[:maxLen-3]) + "..."
}

func GetCategoryStr(category types.OrderCategory) string {
	if category == types.LongOrderCategory {
		return "Long"
	}
	return "Short"
}

// 辅助函数
func GetString(m map[string]interface{}, key string) string {
	if val, ok := m[key]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}

func GetFloat64(m map[string]interface{}, key string) float64 {
	if val, ok := m[key]; ok {
		switch v := val.(type) {
		case float64:
			return v
		case int:
			return float64(v)
		case int64:
			return float64(v)
		}
	}
	return 0
}

func GetInt64(m map[string]interface{}, key string) int64 {
	if val, ok := m[key]; ok {
		switch v := val.(type) {
		case int64:
			return v
		case int:
			return int64(v)
		case float64:
			return int64(v)
		}
	}
	return 0
}

func GetInt32(m map[string]interface{}, key string) int32 {
	if val, ok := m[key]; ok {
		switch v := val.(type) {
		case int32:
			return v
		case int:
			return int32(v)
		case int64:
			return int32(v)
		case float64:
			return int32(v)
		}
	}
	return 0
}
