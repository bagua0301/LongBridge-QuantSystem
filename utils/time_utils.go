// Package time_utils 提供可缩放时间的虚拟时钟，用于模拟/回测中加速或减速时间流逝。
package utils

import (
	"sync"
	"time"
)

// VirtualClock 虚拟时钟，支持时间缩放因子。
// 缩放因子 scale 表示 1 秒真实时间对应多少秒虚拟时间。
// 例如 scale=5 表示真实等待 1 秒，虚拟时间前进 5 秒。
type VirtualClock struct {
	mu          sync.Mutex
	scale       float64   // 时间缩放因子（虚拟秒 / 真实秒）
	realStart   time.Time // 时钟创建时的真实时间（UTC）
	virtualBase time.Time // 时钟创建时的虚拟基准时间
}

// NewVirtualClock 创建新的虚拟时钟。
// scale: 缩放因子，必须 > 0。例如 5 表示时间加速 5 倍。
// virtualStart: 虚拟起始时间，通常传入 time.Now() 或固定时间点。
func NewVirtualClock(scale float64, virtualStart time.Time) *VirtualClock {
	if scale <= 0 {
		scale = 1
	}
	return &VirtualClock{
		scale:       scale,
		realStart:   time.Now(),
		virtualBase: virtualStart,
	}
}

// Scale 返回当前缩放因子。
func (vc *VirtualClock) Scale() float64 {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	return vc.scale
}

// SetScale 动态修改缩放因子（线程安全）。
func (vc *VirtualClock) SetScale(scale float64) {
	if scale <= 0 {
		scale = 1
	}
	vc.mu.Lock()
	defer vc.mu.Unlock()
	// 调整基准，使 Now() 连续
	nowReal := time.Now()
	elapsedReal := nowReal.Sub(vc.realStart)
	elapsedVirtual := time.Duration(float64(elapsedReal) * vc.scale)
	newVirtualNow := vc.virtualBase.Add(elapsedVirtual)
	vc.realStart = nowReal
	vc.virtualBase = newVirtualNow
	vc.scale = scale
}

func (vc *VirtualClock) Set(virtualTime time.Time) {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	vc.realStart = time.Now()
	vc.virtualBase = virtualTime
}

// Now 返回当前虚拟时间。
func (vc *VirtualClock) Now() time.Time {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	elapsedReal := time.Since(vc.realStart)
	elapsedVirtual := time.Duration(float64(elapsedReal) * vc.scale)
	return vc.virtualBase.Add(elapsedVirtual)
}

// Sleep 休眠指定的虚拟时长（实际休眠时间 = 虚拟时长 / 缩放因子）。
func (vc *VirtualClock) Sleep(virtualDur time.Duration) {
	realDur := time.Duration(float64(virtualDur) / vc.Scale())
	time.Sleep(realDur)
}

// Since 返回从指定虚拟时间点到当前虚拟时间点经过的时间。
func (vc *VirtualClock) Since(t time.Time) time.Duration {
	return vc.Now().Sub(t)
}

// Until 返回从当前虚拟时间点到指定虚拟时间点剩余的时间。
func (vc *VirtualClock) Until(t time.Time) time.Duration {
	return t.Sub(vc.Now())
}

// Tick 返回一个虚拟的 ticker 通道，每隔 virtualInterval 虚拟时间发送一次时间戳。
// 实际内部使用真实 timer 缩放实现。
func (vc *VirtualClock) Tick(virtualInterval time.Duration) <-chan time.Time {
	ch := make(chan time.Time)
	realInterval := time.Duration(float64(virtualInterval) / vc.Scale())
	go func() {
		ticker := time.NewTicker(realInterval)
		defer ticker.Stop()
		for tReal := range ticker.C {
			// 计算当前虚拟时间
			vc.mu.Lock()
			elapsedReal := tReal.Sub(vc.realStart)
			elapsedVirtual := time.Duration(float64(elapsedReal) * vc.scale)
			virtualNow := vc.virtualBase.Add(elapsedVirtual)
			vc.mu.Unlock()
			ch <- virtualNow
		}
	}()
	return ch
}

// 全局默认虚拟时钟（便于简单使用）
var defaultClock *VirtualClock

func VirtualClockInit() {
	defaultClock = NewVirtualClock(1, time.Now())
}

// SetScale 设置全局默认时钟的缩放因子。
func VirtualClockSetScale(scale float64) {
	defaultClock.SetScale(scale)
}

// VirtualClockSet 设置全局默认虚拟时钟的当前时间。
func VirtualClockSet(virtualTime time.Time) {
	if defaultClock == nil {
		VirtualClockInit()
	}
	defaultClock.Set(virtualTime)
}

// Now 返回全局虚拟时钟的当前时间。
func Now() time.Time {
	return defaultClock.Now()
}

// Sleep 使用全局虚拟时钟休眠。
func Sleep(d time.Duration) {
	defaultClock.Sleep(d)
}

func Since(t time.Time) time.Duration {
	return defaultClock.Since(t)
}

// 将 float64 秒时间戳转为 time.Time（保留纳秒精度）
func Float64ToTime(timestampSec int64) time.Time {
	sec := int64(timestampSec)                // 整数秒部分
	nsec := int64((timestampSec - sec) * 1e9) // 小数部分转纳秒
	return time.Unix(sec, nsec)
}
