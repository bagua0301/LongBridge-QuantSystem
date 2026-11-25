package quant

import (
	"math"
	"sync"
	"time"

	"LongBridge-QuantSystem/config"
	"LongBridge-QuantSystem/logger"
	"LongBridge-QuantSystem/types"
	"LongBridge-QuantSystem/utils"

	"github.com/longportapp/openapi-go/quote"
)

// 秒级历史数据（包含原始K线和MACD指标）
type SecondHistory struct {
	mu        sync.RWMutex
	data      []*types.SecondData // 秒级K线
	macd      []*types.MACDData   // 秒级MACD序列
	limit     int                 // 最大保留秒数（默认3600）
	appConfig *config.AppConfig

	lastEMA12     float64
	lastEMA26     float64
	lastSignal    float64
	isInitialized bool // 标记是否已完成初次计算

	lastVolume    int64 // 上轮成交量
	lastTimestamp int64 // 上轮时间

	updateCount int // 记录更新次数
}

// 创建秒级历史管理器
func NewSecondHistory(appConfig *config.AppConfig, limit int) *SecondHistory {
	if limit <= 0 {
		limit = 3600 // 1小时
	}
	return &SecondHistory{
		data:      make([]*types.SecondData, 0),
		macd:      make([]*types.MACDData, 0),
		limit:     limit,
		appConfig: appConfig,

		lastEMA12:     0,
		lastEMA26:     0,
		lastSignal:    0,
		isInitialized: false,

		lastVolume:    0,
		lastTimestamp: 0,

		updateCount: 0,
	}
}

// -------------------- 内部方法 --------------------
func (m *SecondHistory) updateData(price float64, ev *quote.PushQuote) {
	if ev.Turnover == nil {
		return
	}
	turnover, ok := utils.DecimalToFloat64(ev.Turnover)
	if !ok {
		logger.Warn("总成交额转换失败 %s: 原始值=%s", ev.Symbol, ev.Turnover.String())
		return
	}
	ts := ev.Timestamp
	if len(m.data) == 0 || m.data[len(m.data)-1].Timestamp != ts {
		if m.lastVolume == 0 {
			m.lastVolume = ev.Volume
		}
		data := &types.SecondData{
			Timestamp:     ts,
			Time:          time.Unix(ts, 0),
			Open:          price,
			High:          price,
			Low:           price,
			Close:         price,
			Volume:        m.lastVolume,
			Turnover:      turnover,
			CloseVolume:   ev.Volume,
			CloseTurnover: turnover,
		}
		m.data = append(m.data, data)
	} else {
		// 更新当前秒
		last := m.data[len(m.data)-1]
		if price > last.High {
			last.High = price
		}
		if price < last.Low {
			last.Low = price
		}
		last.Close = price
		last.CloseVolume = ev.Volume
		last.CloseTurnover = turnover

		m.lastVolume = ev.Volume
	}

	// 限制数据量
	if len(m.data) > m.limit {
		diff := len(m.data) - m.limit
		for i := 0; i < diff; i++ {
			m.data[i] = nil // 关键：手动解除引用
		}
		m.data = m.data[diff:]
	}
}

func (m *SecondHistory) updateMACD() {
	if len(m.data) < m.appConfig.MACDSlow {
		return
	}
	closes := make([]float64, len(m.data))
	timestamps := make([]int64, len(m.data))
	for i, d := range m.data {
		closes[i] = d.Close
		timestamps[i] = d.Timestamp
	}

	ema12 := utils.CalculateEMA(closes, m.appConfig.MACDFast)
	ema26 := utils.CalculateEMA(closes, m.appConfig.MACDSlow)
	macdLine := make([]float64, len(closes))
	for i := range closes {
		macdLine[i] = ema12[i] - ema26[i]
	}
	signalLine := utils.CalculateEMA(macdLine, m.appConfig.MACDSignal)

	macdList := make([]*types.MACDData, 0, len(m.data))
	for i, ts := range timestamps {
		macdList = append(macdList, &types.MACDData{
			Timestamp: ts,
			MACD:      macdLine[i],
			Signal:    signalLine[i],
			Histogram: macdLine[i] - signalLine[i],
		})
	}
	if len(macdList) > m.limit {
		macdList = macdList[len(macdList)-m.limit:]
	}
	m.macd = macdList

	if len(ema12) > 0 {
		m.lastEMA12 = ema12[len(ema12)-1]
		m.lastEMA26 = ema26[len(ema26)-1]
		m.lastSignal = signalLine[len(signalLine)-1]
		m.isInitialized = true
	}
}

func (m *SecondHistory) updateMACDIncremental(price float64, timeStamp int64) {
	if !m.isInitialized {
		m.updateMACD()
		m.lastTimestamp = timeStamp
		return
	}

	alphaFast := 2.0 / (float64(m.appConfig.MACDFast) + 1.0)
	alphaSlow := 2.0 / (float64(m.appConfig.MACDSlow) + 1.0)
	alphaSignal := 2.0 / (float64(m.appConfig.MACDSignal) + 1.0)

	currentEMA12 := price*alphaFast + m.lastEMA12*(1.0-alphaFast)
	currentEMA26 := price*alphaSlow + m.lastEMA26*(1.0-alphaSlow)
	diff := currentEMA12 - currentEMA26
	currentSignal := diff*alphaSignal + m.lastSignal*(1.0-alphaSignal)

	newMACD := &types.MACDData{
		Timestamp: timeStamp,
		MACD:      diff,
		Signal:    currentSignal,
		Histogram: diff - currentSignal,
	}
	logger.Debug("SecondHistory Timestamp=%d, MACD=%.4f, Signal=%.4f, Histogram=%.4f",
		newMACD.Timestamp, newMACD.MACD, newMACD.Signal, newMACD.Histogram)

	if m.lastTimestamp != 0 && timeStamp != m.lastTimestamp {
		m.lastEMA12 = currentEMA12
		m.lastEMA26 = currentEMA26
		m.lastSignal = currentSignal

		m.macd = append(m.macd, newMACD)
		m.lastTimestamp = timeStamp
	} else {
		// --- 分钟内：更新当前分钟的“预览值” ---
		if len(m.macd) > 0 {
			// 覆盖最后一个 MACD 节点，使其与当前正在变动的 K 线对齐
			m.macd[len(m.macd)-1] = newMACD
		} else {
			// 如果 macd 为空则追加
			m.macd = append(m.macd, newMACD)
		}
	}

	if len(m.macd) > m.limit {
		diff := len(m.macd) - m.limit
		for i := 0; i < diff; i++ {
			m.macd[i] = nil
		}
		m.macd = m.macd[diff:]
	}
}

// 清除数据
func (m *SecondHistory) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.data = make([]*types.SecondData, 0)
	m.macd = make([]*types.MACDData, 0)
}

// 更新秒级数据
func (m *SecondHistory) Update(price float64, ev *quote.PushQuote) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.updateData(price, ev)

	// 策略：每N秒推播或检测到跨天/跨时段，强制全量计算校准一次
	// 这样可以彻底消除 EMA 增量计算积累的浮点误差
	if m.lastTimestamp != 0 && ev.Timestamp != m.lastTimestamp {
		m.updateCount++
		if ev.Timestamp-m.lastTimestamp > 60 || m.updateCount >= m.appConfig.MACDSecondUpdateCount {
			m.updateMACD() // 全量计算会重置 lastEMA12/26 和 lastSignal
			m.updateCount = 0
			return
		}
	}

	// 增量MACD
	m.updateMACDIncremental(price, ev.Timestamp)
}

// 获取秒级数据
func (m *SecondHistory) GetData(num int) []*types.SecondData {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if num <= 0 || num >= len(m.data) {
		return m.data
	}
	return m.data[len(m.data)-num:]
}

// 获取秒级MACD序列
func (m *SecondHistory) GetMACD(num int) []*types.MACDData {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.macd) <= 0 || len(m.macd) <= num+1 {
		return make([]*types.MACDData, 0)
	}
	return m.macd[len(m.macd)-num-1:]
}

// 获取最新MACD值
func (m *SecondHistory) GetLatestMACD() (*types.MACDData, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.macd) == 0 {
		return nil, false
	}
	return m.macd[len(m.macd)-1], true
}

// 获取金叉(1)/死叉(-1)/无信号(0)
func (m *SecondHistory) GetMACDSignal() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.macd) < 2 {
		return 0
	}
	prev := m.macd[len(m.macd)-2]
	curr := m.macd[len(m.macd)-1]
	if prev.MACD <= prev.Signal && curr.MACD > curr.Signal {
		return 1
	}
	if prev.MACD >= prev.Signal && curr.MACD < curr.Signal {
		return -1
	}
	return 0
}

// 获取最新柱状图值
func (m *SecondHistory) GetMACDStrength() float64 {
	latest, ok := m.GetLatestMACD()
	if !ok {
		return 0
	}
	return latest.Histogram
}

// 获取当前的成交量
func (m *SecondHistory) GetCurrentVolume(exNum int) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.data) <= exNum {
		return 0
	}
	d := m.data[int64(len(m.data)-exNum-1)]
	if d.CloseVolume < d.Volume { // 数据异常保护
		return 0
	}
	vol := float64(d.CloseVolume - d.Volume)
	if vol < 0 {
		return 0
	}
	return vol
}

// 获取最近N个级别平均价格
func (m *SecondHistory) GetAveragePrice(num int, exNum int) float64 {
	if num <= 0 || exNum < 0 {
		return 0
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	n := len(m.data)
	if n < num+exNum {
		return 0
	}

	start := n - num - exNum // 排除当前未走完的最后exNum根
	if start < 0 {
		start = 0
	}

	var sum float64
	for i := start; i < n-1; i++ {
		sum += (m.data[i].Open + m.data[i].Close) / 2
	}
	return sum / float64(num)
}

// 获取过去N个级别平均成交量
func (m *SecondHistory) GetAverageVolume(num int, exNum int) float64 {
	if num <= 0 || exNum < 0 {
		return 0
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	n := len(m.data)
	if n < num+exNum {
		return 0
	}

	var totalVolume float64 = 0
	for i := int64(n - num - exNum); i < int64(n-exNum-1); i++ {
		// 净增量 = 当前累计 - 开盘累计
		vol := float64(m.data[i].CloseVolume - m.data[i].Volume)
		if vol < 0 {
			vol = 0
		}
		totalVolume += vol
	}
	return totalVolume / float64(num)
}

// 获取价格变化百分比
func (m *SecondHistory) GetPriceChangePercent(num int) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.data) < 2 {
		return 0
	}

	firstClose := m.data[0].Close
	lastClose := m.data[len(m.data)-1].Close
	return (lastClose - firstClose) / firstClose * 100
}

// 获取指定窗口内的最高价
func (m *SecondHistory) GetHighPrice(num int) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.data) == 0 {
		return 0
	}

	high := m.data[0].High
	for _, data := range m.data {
		if data.High > high {
			high = data.High
		}
	}
	return high
}

// 检查价格是否突破近期高点
func (m *SecondHistory) IsPriceBreakingHigh(currentPrice float64, num int) bool {
	high := m.GetHighPrice(num)
	return currentPrice > high
}

// 获取指定周期数的ATR（平均真实波幅）
func (m *SecondHistory) GetATR(num int, exNum int) float64 {
	if num < 1 || exNum < 0 {
		return 0
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	// 去除N,避免不准(一般最后1根不完整)
	if len(m.data) < num+exNum {
		return 0
	}

	sum := 0.0
	for i := len(m.data) - num; i < len(m.data)-exNum-1; i++ {
		sum += math.Max(m.data[i].High-m.data[i].Low,
			math.Max(math.Abs(m.data[i].High-m.data[i-1].Close),
				math.Abs(m.data[i].Low-m.data[i-1].Close)))
	}
	return sum / float64(num)
}

// 计算一组价格序列的 RSI
func (m *SecondHistory) GetRSI(num int, exNum int) float64 {
	if num <= 0 || exNum < 0 {
		return 50.0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	n := len(m.data)
	effectiveLen := n - exNum // 排除当前正在变动的最后1根
	if effectiveLen <= num {
		return 50.0
	}

	maxLookback := num * 3
	startIdx := 1 // 确保 startIdx 至少为 1，防止 i-1 越界
	if effectiveLen > maxLookback {
		startIdx = effectiveLen - maxLookback
	}
	// 二次加固，防止 maxLookback 计算导致的逻辑异常
	if startIdx < 1 {
		startIdx = 1
	}

	var gains, losses float64
	// 初始化第一个周期
	for i := startIdx; i < startIdx+num; i++ {
		change := m.data[i].Close - m.data[i-1].Close
		if change > 0 {
			gains += change
		} else {
			losses -= change
		}
	}
	avgGain := gains / float64(num)
	avgLoss := losses / float64(num)

	// Wilder's Smoothing
	for i := startIdx + num; i < effectiveLen; i++ {
		change := m.data[i].Close - m.data[i-1].Close
		var curGain, curLoss float64
		if change > 0 {
			curGain = change
		} else {
			curLoss = -change
		}
		avgGain = (avgGain*float64(num-1) + curGain) / float64(num)
		avgLoss = (avgLoss*float64(num-1) + curLoss) / float64(num)
	}

	if avgLoss == 0 {
		return 100.0
	}
	return 100.0 - (100.0 / (1.0 + (avgGain / avgLoss)))
}
