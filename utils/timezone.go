package utils

import (
	"fmt"
	"time"
)

var (
	beijingLoc *time.Location
	newYorkLoc *time.Location
)

func init() {
	var err error

	// 加载北京时区
	beijingLoc, err = time.LoadLocation("Asia/Shanghai")
	if err != nil {
		beijingLoc = time.FixedZone("CST", 8*3600)
	}

	// 加载美东时区
	newYorkLoc, err = time.LoadLocation("America/New_York")
	if err != nil {
		newYorkLoc = time.FixedZone("EST", -5*3600)
	}
}

// GetBeijingTime 获取北京时间
func GetBeijingTime() time.Time {
	return time.Now().In(beijingLoc)
}

// GetNewYorkTime 获取美东时间
func GetNewYorkTime() time.Time {
	return time.Now().In(newYorkLoc)
}

// ConvertToNewYork 转换为美东时间
func ConvertToNewYork(t time.Time) time.Time {
	return t.In(newYorkLoc)
}

// ConvertToBeijing 转换为北京时间
func ConvertToBeijing(t time.Time) time.Time {
	return t.In(beijingLoc)
}

// GetTimeDifference 获取时差（小时）
func GetTimeDifference() int {
	now := time.Now()
	_, beijingOffset := now.In(beijingLoc).Zone()
	_, nyOffset := now.In(newYorkLoc).Zone()
	return (beijingOffset - nyOffset) / 3600
}

// IsDaylightSaving 判断美东是否夏令时
func IsDaylightSaving() bool {
	_, offset := GetNewYorkTime().Zone()
	return offset == -14400 // 夏令时 UTC-4
}

// GetTimeInfo 获取详细时间信息
func GetTimeInfo() map[string]interface{} {
	beijing := GetBeijingTime()
	newYork := GetNewYorkTime()

	_, beijingOffset := beijing.Zone()
	_, nyOffset := newYork.Zone()

	return map[string]interface{}{
		"beijing_time":       beijing.Format("2006-01-02 15:04:05"),
		"newyork_time":       newYork.Format("2006-01-02 15:04:05"),
		"timezone_diff":      fmt.Sprintf("%d小时", (beijingOffset-nyOffset)/3600),
		"is_daylight_saving": IsDaylightSaving(),
		"beijing_offset":     fmt.Sprintf("UTC%+d", beijingOffset/3600),
		"newyork_offset":     fmt.Sprintf("UTC%+d", nyOffset/3600),
	}
}
