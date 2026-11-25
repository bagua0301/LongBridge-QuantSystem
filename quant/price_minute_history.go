package quant

import (
	"fmt"
	"math"
	"os"
	"sync"
	"time"

	"LongBridge-QuantSystem/config"
	"LongBridge-QuantSystem/logger"
	"LongBridge-QuantSystem/types"
	"LongBridge-QuantSystem/utils"

	"github.com/longportapp/openapi-go/quote"
	"github.com/wcharczuk/go-chart/v2"
)

// 分钟历史数据（包含原始K线和MACD指标）
type MinuteHistory struct {
	mu        sync.RWMutex
	data      []*types.MinuteData // 分钟数据
	macd      []*types.MACDData   // MACD指标
	limit     int                 // 最大保留分钟数
	appConfig *config.AppConfig

	lastEMA12     float64
	lastEMA26     float64
	lastSignal    float64
	isInitialized bool // 标记是否已完成初次计算

	lastVolume    int64 // 上轮成交量
	lastTimestamp int64 // 上轮时间

	updateCount int // 记录更新次数
}

// 创建分钟历史管理器
func NewMinuteHistory(appConfig *config.AppConfig, limit int) *MinuteHistory {
	if limit <= 0 {
		limit = 10080 // 默认保留7天（60*24*7）
	}
	return &MinuteHistory{
		data:      make([]*types.MinuteData, 0, limit),
		macd:      make([]*types.MACDData, 0, limit),
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
func (m *MinuteHistory) updateData(price float64, ev *quote.PushQuote) {
	if ev.Turnover == nil {
		return
	}
	turnover, ok := utils.DecimalToFloat64(ev.Turnover)
	if !ok {
		logger.Warn("总成交额转换失败 %s: 原始值=%s", ev.Symbol, ev.Turnover.String())
		return
	}
	now := time.Unix(ev.Timestamp, 0)
	currentMinute := now.Truncate(time.Minute)
	minuteTimestamp := currentMinute.Unix()

	if len(m.data) == 0 || m.data[len(m.data)-1].Timestamp != minuteTimestamp {
		if m.lastVolume == 0 {
			m.lastVolume = ev.Volume
		}
		// 新的一分钟
		newMinute := &types.MinuteData{
			Timestamp:     minuteTimestamp,
			Time:          currentMinute,
			Open:          price,
			High:          price,
			Low:           price,
			Close:         price,
			Volume:        ev.Volume,
			Turnover:      turnover,
			CloseVolume:   ev.Volume,
			CloseTurnover: turnover,
		}
		m.data = append(m.data, newMinute)
	} else {
		// 更新当前分钟
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

func (m *MinuteHistory) updateMACD() {
	if len(m.data) < m.appConfig.MACDSlow {
		return
	}
	closes := make([]float64, len(m.data))
	timestamps := make([]int64, len(m.data))
	for i, d := range m.data {
		closes[i] = d.Close
		timestamps[i] = time.Unix(d.Timestamp, 0).Truncate(time.Minute).Unix()
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

func (m *MinuteHistory) updateMACDIncremental(price float64, timeStamp int64) {
	nowMinute := time.Unix(timeStamp, 0).Truncate(time.Minute).Unix()
	if !m.isInitialized {
		m.updateMACD()
		m.lastTimestamp = nowMinute
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
		Timestamp: nowMinute,
		MACD:      diff,
		Signal:    currentSignal,
		Histogram: diff - currentSignal,
	}
	logger.Info("MinuteHistory Timestamp=%d, MACD=%.4f, Signal=%.4f, Histogram=%.4f",
		newMACD.Timestamp, newMACD.MACD, newMACD.Signal, newMACD.Histogram)

	if m.lastTimestamp != 0 && nowMinute != m.lastTimestamp {
		m.lastEMA12 = currentEMA12
		m.lastEMA26 = currentEMA26
		m.lastSignal = currentSignal

		m.macd = append(m.macd, newMACD)
		m.lastTimestamp = nowMinute
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
func (m *MinuteHistory) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.data = make([]*types.MinuteData, 0, m.limit)
	m.macd = make([]*types.MACDData, 0, m.limit)
}

// 将MACD数据绘制并保存为图片
func (m *MinuteHistory) DrawMACD(filePath string, allOrders []*types.CompletedOrder) error {
	var xValues []float64
	var macdValues []float64
	var signalValues []float64
	var histValues []float64

	minuteMap := make(map[int64]float64)
	for _, v := range m.macd {
		xValues = append(xValues, float64(v.Timestamp))
		macdValues = append(macdValues, v.MACD)
		signalValues = append(signalValues, v.Signal)
		histValues = append(histValues, v.Histogram)

		minuteMap[v.Timestamp] = v.MACD
	}

	// 2. 准备买卖点数据
	var buyX, buyY []float64
	var sellX, sellY []float64

	for _, v := range allOrders {
		// 按分钟存储,取对应分钟
		ts := v.Timestamp.Truncate(time.Minute).Unix()

		yVal, exists := minuteMap[ts]
		if !exists {
			logger.Error("not find time OrderID=%s, ts=%d", v.OrderID, ts)
			continue
		}

		if v.Action == types.BuyOrderAction {
			buyX = append(buyX, float64(ts))
			buyY = append(buyY, yVal)
		} else {
			sellX = append(sellX, float64(ts))
			sellY = append(sellY, yVal)
		}
	}

	graph := chart.Chart{
		Series: []chart.Series{
			// MACD 线
			chart.ContinuousSeries{
				Name:    "MACD",
				XValues: xValues,
				YValues: macdValues,
				Style:   chart.Style{StrokeColor: chart.ColorBlue, StrokeWidth: 2},
			},
			// 信号线
			chart.ContinuousSeries{
				Name:    "Signal",
				XValues: xValues,
				YValues: signalValues,
				Style:   chart.Style{StrokeColor: chart.ColorRed, StrokeWidth: 2},
			},
			// 柱状图 (用散点模拟或增加自定义逻辑)
			chart.ContinuousSeries{
				Name:    "Histogram",
				XValues: xValues,
				YValues: histValues,
				Style:   chart.Style{StrokeColor: chart.ColorGreen, StrokeWidth: 1},
			},
			// 买入点：绿色散点
			chart.ContinuousSeries{
				Name: "Buy Orders",
				Style: chart.Style{
					StrokeWidth: chart.Disabled,
					DotWidth:    5,
					DotColor:    chart.ColorGreen,
				},
				XValues: buyX,
				YValues: buyY,
			},
			// 卖出点：红色散点
			chart.ContinuousSeries{
				Name: "Sell Orders",
				Style: chart.Style{
					StrokeWidth: chart.Disabled,
					DotWidth:    5,
					DotColor:    chart.ColorRed,
				},
				XValues: sellX,
				YValues: sellY,
			},
		},
	}

	f, _ := os.Create(filePath)
	defer f.Close()
	return graph.Render(chart.PNG, f)
}

// 将价格数据绘制并保存为图片
func (m *MinuteHistory) DrawPrice(filePath string, allOrders []*types.CompletedOrder) error {
	var xValues []float64
	var priceValues []float64

	minuteMap := make(map[int64]float64)
	for _, v := range m.data {
		xValues = append(xValues, float64(v.Timestamp))
		priceValues = append(priceValues, v.Close)

		minuteMap[v.Timestamp] = v.Close
	}

	// 2. 准备买卖点数据
	var buyX, buyY []float64
	var sellX, sellY []float64

	for _, v := range allOrders {
		// 按分钟存储,取对应分钟
		ts := v.Timestamp.Truncate(time.Minute).Unix()

		yVal, exists := minuteMap[ts]
		if !exists {
			logger.Error("not find time OrderID=%s, ts=%d", v.OrderID, ts)
			continue
		}

		if v.Action == types.BuyOrderAction {
			buyX = append(buyX, float64(ts))
			buyY = append(buyY, yVal)
		} else {
			sellX = append(sellX, float64(ts))
			sellY = append(sellY, yVal)
		}
	}

	graph := chart.Chart{
		Series: []chart.Series{
			// price 线
			chart.ContinuousSeries{
				Name:    "price",
				XValues: xValues,
				YValues: priceValues,
				Style:   chart.Style{StrokeColor: chart.ColorBlue, StrokeWidth: 2},
			},
			// 买入点：绿色散点
			chart.ContinuousSeries{
				Name: "Buy Orders",
				Style: chart.Style{
					StrokeWidth: chart.Disabled,
					DotWidth:    5,
					DotColor:    chart.ColorGreen,
				},
				XValues: buyX,
				YValues: buyY,
			},
			// 卖出点：红色散点
			chart.ContinuousSeries{
				Name: "Sell Orders",
				Style: chart.Style{
					StrokeWidth: chart.Disabled,
					DotWidth:    5,
					DotColor:    chart.ColorRed,
				},
				XValues: sellX,
				YValues: sellY,
			},
		},
	}

	f, _ := os.Create(filePath)
	defer f.Close()
	return graph.Render(chart.PNG, f)
}

// 更新分钟级数据
func (m *MinuteHistory) Update(price float64, ev *quote.PushQuote) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.updateData(price, ev)

	// 策略：每N次推播或检测到跨天/跨时段，强制全量计算校准一次
	// 这样可以彻底消除 EMA 增量计算积累的浮点误差
	nowMinute := time.Unix(ev.Timestamp, 0).Truncate(time.Minute).Unix()
	if m.lastTimestamp != 0 && nowMinute != m.lastTimestamp {
		m.updateCount++
		if m.updateCount >= m.appConfig.MACDMinuteUpdateCount {
			m.updateMACD() // 全量计算会重置 lastEMA12/26 和 lastSignal
			m.updateCount = 0
			return
		}
	}

	// 增量MACD
	m.updateMACDIncremental(price, ev.Timestamp)
}

// 获取分钟数据
func (m *MinuteHistory) GetData(num int) []*types.MinuteData {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if num <= 0 || num >= len(m.data) {
		return m.data
	}
	return m.data[len(m.data)-num:]
}

// 获取分钟级MACD序列
func (m *MinuteHistory) GetMACD(num int) []*types.MACDData {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.macd) <= 0 || len(m.macd) <= num+1 {
		return make([]*types.MACDData, 0)
	}
	return m.macd[len(m.macd)-num-1:]
}

// 获取最新MACD值
func (m *MinuteHistory) GetLatestMACD() (*types.MACDData, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.macd) == 0 {
		return nil, false
	}
	return m.macd[len(m.macd)-1], true
}

// 获取金叉/死叉信号
func (m *MinuteHistory) GetMACDSignal() int {
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
func (m *MinuteHistory) GetMACDStrength() float64 {
	latest, ok := m.GetLatestMACD()
	if !ok {
		return 0
	}
	return latest.Histogram
}

// 获取MACD趋势
func (m *MinuteHistory) GetMACDTrend() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.macd) < 3 {
		return string(types.NoneTrend)
	}
	last3 := m.macd[len(m.macd)-3:]
	upCount, downCount := 0, 0
	for i := 1; i < len(last3); i++ {
		if last3[i].MACD > last3[i-1].MACD {
			upCount++
		} else if last3[i].MACD < last3[i-1].MACD {
			downCount++
		}
	}
	if upCount >= 2 {
		return string(types.UpTrend)
	} else if downCount >= 2 {
		return string(types.DownTrend)
	}
	return string(types.NoneTrend)
}

// 获取N分的成交量
func (m *MinuteHistory) GetCurrentVolume(exNum int) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.data) <= exNum {
		return 0
	}
	d := m.data[len(m.data)-exNum-1]
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
func (m *MinuteHistory) GetAveragePrice(num int, exNum int) float64 {
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
func (m *MinuteHistory) GetAverageVolume(num int, exNum int) float64 {
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
func (m *MinuteHistory) GetPriceChangePercent(num int) float64 {
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
func (m *MinuteHistory) GetHighPrice(num int) float64 {
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
func (m *MinuteHistory) IsPriceBreakingHigh(currentPrice float64, num int) bool {
	high := m.GetHighPrice(num)
	return currentPrice > high
}

// 获取指定周期数的ATR（平均真实波幅）
func (m *MinuteHistory) GetATR(num int, exNum int) float64 {
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

// GetRSI 计算一组价格序列的 RSI（外部调用，带读锁）
func (m *MinuteHistory) GetRSI(num int, exNum int) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.getRSI(num, exNum)
}

// getRSI 内部无锁版本（由 CheckSignals 或其它已持有锁的调用者使用）
func (m *MinuteHistory) getRSI(num int, exNum int) float64 {
	if num <= 0 || exNum < 0 {
		return 50.0
	}

	n := len(m.data)
	effectiveLen := n - exNum // 排除当前正在变动的最后1根
	if effectiveLen <= num {
		return 50.0
	}

	maxLookback := num * m.appConfig.SignalRSIBase
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

// ================== 辅助方法 ==================
// 计算简单移动平均
func (m *MinuteHistory) getSMA(idx int, num int) float64 {
	if idx < num-1 || idx >= len(m.data) {
		return 0
	}
	sum := 0.0
	for i := idx - num + 1; i <= idx; i++ {
		sum += m.data[i].Close
	}
	return sum / float64(num)
}

// 判断是否处于上升趋势（价格 > SMA60 且 SMA60 向上）
func (m *MinuteHistory) isUptrend(idx int) bool {
	ma60 := m.appConfig.SignalWindow
	if idx < ma60 {
		ma60 = len(m.data) - 1
	}
	smaNow := m.getSMA(idx, ma60)
	smaPrev := m.getSMA(idx-1, ma60)
	return m.data[idx].Close > smaNow && smaNow > smaPrev
}

// 判断是否处于下降趋势（价格 < SMA60 且 SMA60 向下）
func (m *MinuteHistory) isDowntrend(idx int) bool {
	ma60 := m.appConfig.SignalWindow
	if idx < ma60 {
		ma60 = len(m.data) - 1
	}
	smaNow := m.getSMA(idx, ma60)
	smaPrev := m.getSMA(idx-1, ma60)
	return m.data[idx].Close < smaNow && smaNow < smaPrev
}

// 确认金叉（需要前后两根K线都满足 MACD > Signal，且之前处于下方）
func (m *MinuteHistory) isGoldenCrossConfirmed(lastIdx int) bool {
	if lastIdx < 2 || lastIdx >= len(m.macd) {
		return false
	}
	prev := m.macd[lastIdx-1]
	curr := m.macd[lastIdx]
	// 交叉确认：前一根MACD <= Signal，当前MACD > Signal
	// 额外要求前一根的MACD确实在Signal下方（避免盘整频繁穿越）
	if prev.MACD <= prev.Signal && curr.MACD > curr.Signal {
		// 可选：检查更早一根也低于信号线，提升可靠性
		if lastIdx >= 2 && m.macd[lastIdx-2].MACD <= m.macd[lastIdx-2].Signal {
			return true
		}
		// 至少前一根在下方
		return true
	}
	return false
}

// 确认死叉
func (m *MinuteHistory) isDeathCrossConfirmed(lastIdx int) bool {
	if lastIdx < 2 || lastIdx >= len(m.macd) {
		return false
	}
	prev := m.macd[lastIdx-1]
	curr := m.macd[lastIdx]
	if prev.MACD >= prev.Signal && curr.MACD < curr.Signal {
		if lastIdx >= 2 && m.macd[lastIdx-2].MACD >= m.macd[lastIdx-2].Signal {
			return true
		}
		return true
	}
	return false
}

// 动态成交量放大（基于均值 + N倍标准差）
func (m *MinuteHistory) isVolumeSurgeDynamic(idx int, num int, stdDevs float64) bool {
	if idx < num || idx >= len(m.data) {
		return false
	}
	vols := make([]float64, num)
	for i := 0; i < num; i++ {
		vol := float64(m.data[idx-i].CloseVolume - m.data[idx-i].Volume)
		if vol < 0 {
			vol = 0
		}
		vols[num-1-i] = vol
	}
	// 计算均值与标准差
	var sum float64
	for _, v := range vols {
		sum += v
	}
	mean := sum / float64(num)
	var sqDiff float64
	for _, v := range vols {
		diff := v - mean
		sqDiff += diff * diff
	}
	stdDev := math.Sqrt(sqDiff / float64(num))
	currentVol := float64(m.data[idx].CloseVolume - m.data[idx].Volume)
	if currentVol < 0 {
		currentVol = 0
	}
	return currentVol > mean+stdDevs*stdDev
}

// 底部反转K线形态（锤子线或看涨吞没）
func (m *MinuteHistory) isBullishReversalCandle(idx int) bool {
	if idx < 1 || idx >= len(m.data) {
		return false
	}
	curr := m.data[idx]
	prev := m.data[idx-1]
	// 简单锤子线：下影线长度 > 实体长度2倍，且收盘价靠近最高价
	body := math.Abs(curr.Close - curr.Open)
	lowerShadow := curr.Low - math.Min(curr.Open, curr.Close)
	if body > 0 && lowerShadow > body*2 && curr.Close > curr.Open {
		return true
	}
	// 看涨吞没：当前阳线完全覆盖前一根阴线
	if curr.Close > curr.Open && prev.Close < prev.Open && curr.Open < prev.Close && curr.Close > prev.Open {
		return true
	}
	return false
}

// 顶部反转K线形态（倒锤子或看跌吞没）
func (m *MinuteHistory) isBearishReversalCandle(idx int) bool {
	if idx < 1 || idx >= len(m.data) {
		return false
	}
	curr := m.data[idx]
	prev := m.data[idx-1]
	// 倒锤子：上影线长度 > 实体长度2倍，收盘价靠近最低价
	body := math.Abs(curr.Close - curr.Open)
	upperShadow := curr.High - math.Max(curr.Open, curr.Close)
	if body > 0 && upperShadow > body*2 && curr.Close < curr.Open {
		return true
	}
	// 看跌吞没：当前阴线完全覆盖前一根阳线
	if curr.Close < curr.Open && prev.Close > prev.Open && curr.Open > prev.Close && curr.Close < prev.Open {
		return true
	}
	return false
}

func (m *MinuteHistory) isVolumeSurge(idx int, num int, factor float64) bool {
	if idx < num || idx >= len(m.data) {
		return false
	}
	sum := int64(0)
	for i := idx - num; i < idx; i++ {
		vol := m.data[i].CloseVolume - m.data[i].Volume
		if vol < 0 {
			vol = 0
		}
		sum += vol
	}
	avgVol := float64(sum) / float64(num)
	currentVol := float64(m.data[idx].CloseVolume - m.data[idx].Volume)
	if currentVol < 0 {
		currentVol = 0
	}
	return currentVol > avgVol*factor
}

// 判断当前分钟是否量增价升
func (m *MinuteHistory) isVolumeUpPriceUp(idx int) bool {
	if idx < 1 || idx >= len(m.data) {
		return false
	}
	priceUp := m.data[idx].Close > m.data[idx-1].Close
	volUp := (m.data[idx].CloseVolume - m.data[idx].Volume) > (m.data[idx-1].CloseVolume - m.data[idx-1].Volume)
	return priceUp && volUp
}

// 判断当前价格是否处于近期高位
// num: 回看分钟数，threshold: 阈值（如0.95表示价格 >= 最高价的95%）
func (m *MinuteHistory) isHighPosition(idx int, num int, threshold float64) bool {
	if idx < num || idx >= len(m.data) {
		return false
	}
	highest := m.data[idx-num].High
	for i := idx - num + 1; i <= idx; i++ {
		if m.data[i].High > highest {
			highest = m.data[i].High
		}
	}
	return m.data[idx].Close >= highest*threshold
}

// 判断当前价格是否处于近期低位
func (m *MinuteHistory) isLowPosition(idx int, num int, threshold float64) bool {
	if idx < num || idx >= len(m.data) {
		return false
	}
	lowest := m.data[idx-num].Low
	for i := idx - num + 1; i <= idx; i++ {
		if m.data[i].Low < lowest {
			lowest = m.data[i].Low
		}
	}
	return m.data[idx].Close <= lowest*threshold
}

// 查找指定范围内的波段低点（简单实现：比较前后相邻K线）
func (m *MinuteHistory) findSwingLow(start, end int) []int {
	indices := []int{}
	for i := start + 1; i < end-1; i++ {
		if m.data[i].Low < m.data[i-1].Low && m.data[i].Low < m.data[i+1].Low {
			indices = append(indices, i)
		}
	}
	return indices
}

// 查找波段高点
func (m *MinuteHistory) findSwingHigh(start, end int) []int {
	indices := []int{}
	for i := start + 1; i < end-1; i++ {
		if m.data[i].High > m.data[i-1].High && m.data[i].High > m.data[i+1].High {
			indices = append(indices, i)
		}
	}
	return indices
}

// 检测底背离（价格新低，MACD柱或DIF未新低）
func (m *MinuteHistory) detectBullishDivergence(idx int, num int) bool {
	if idx < num+5 {
		return false
	}
	macdData := m.macd
	if len(m.data) != len(macdData) {
		return false
	}
	swingLows := m.findSwingLow(idx-num, idx)
	if len(swingLows) < 2 {
		return false
	}
	lowIdx1 := swingLows[len(swingLows)-2]
	lowIdx2 := swingLows[len(swingLows)-1]
	priceNewLow := m.data[lowIdx2].Low < m.data[lowIdx1].Low
	macdNotNewLow := macdData[lowIdx2].Histogram > macdData[lowIdx1].Histogram
	return priceNewLow && macdNotNewLow
}

// 检测顶背离（价格新高，MACD柱未新高）
func (m *MinuteHistory) detectBearishDivergence(idx int, num int) bool {
	if idx < num+5 {
		return false
	}
	macdData := m.macd
	if len(m.data) != len(macdData) {
		return false
	}
	swingHighs := m.findSwingHigh(idx-num, idx)
	if len(swingHighs) < 2 {
		return false
	}
	highIdx1 := swingHighs[len(swingHighs)-2]
	highIdx2 := swingHighs[len(swingHighs)-1]
	priceNewHigh := m.data[highIdx2].High > m.data[highIdx1].High
	macdNotNewHigh := macdData[highIdx2].Histogram < macdData[highIdx1].Histogram
	return priceNewHigh && macdNotNewHigh
}

func (m *MinuteHistory) calculateADX(num int) float64 {
	count := num * 3
	if len(m.data) < count {
		return 0
	}
	start := len(m.data) - count
	subset := m.data[start:]
	highs := make([]float64, len(subset))
	lows := make([]float64, len(subset))
	closes := make([]float64, len(subset))
	for i, d := range subset {
		highs[i] = d.High
		lows[i] = d.Low
		closes[i] = d.Close
	}
	return utils.CalculateADX(highs, lows, closes, num)
}

// CheckSignals 极致降噪、支持多空切换（1=纯多, 2=纯空）的终极策略
func (m *MinuteHistory) CheckSignals() types.SignalResult {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 1. 基础数据量保护
	signalWindow := m.appConfig.SignalWindow
	if signalWindow <= 0 {
		signalWindow = 30
	}
	if len(m.data) < signalWindow || len(m.macd) < signalWindow {
		return types.SignalResult{Action: types.NoneOrderAction, Reason: "数据量不足"}
	}

	// 2. 严格的时间与收盘 K 线过滤
	now := utils.Now()
	currentMinuteTime := now.Truncate(time.Minute).Unix()

	// 核心安全改良：为了彻底绝杀分钟内 Tick 闪烁导致的“6秒假死叉/假金叉”，
	// 我们将主信号锚定在【绝对闭合】的前一根K线（即收盘线）上。
	lastIdx := len(m.data) - 2
	if now.Unix() >= currentMinuteTime+int64(m.appConfig.SignalCurrentSec) {
		lastIdx = len(m.data) - 1
	}

	// 确保索引安全
	if lastIdx < 2 || lastIdx >= len(m.macd) {
		return types.SignalResult{Action: types.NoneOrderAction, Reason: "有效索引不足"}
	}

	// 3. 读取方向控制开关 SignalTrendType
	signalType := types.SignalTrendType(m.appConfig.SignalType)
	if signalType <= types.SignalTrendMin || signalType >= types.SignalTrendMax {
		signalType = types.SignalTrendLong
	}

	// 4. 计算大周期趋势 (基于 SMA)
	maNow := m.getSMA(lastIdx, signalWindow)
	maPrev := m.getSMA(lastIdx-1, signalWindow)
	currentPrice := m.data[lastIdx].Close

	// 趋势判定
	isPriceUptrend := currentPrice > maNow && maNow > maPrev
	isPriceDowntrend := currentPrice < maNow && maNow < maPrev

	// 5. 提取闭合周期的技术因子
	macdCurr := m.macd[lastIdx]
	macdPrev := m.macd[lastIdx-1]

	// 闭合确认的金叉/死叉
	rawGoldenCross := m.isGoldenCrossConfirmed(lastIdx)
	rawDeathCross := m.isDeathCrossConfirmed(lastIdx)

	volSurge := m.isVolumeSurgeDynamic(lastIdx, m.appConfig.SignalVolumeDynamic, m.appConfig.SignalVolumeFactor)
	rsiVal := m.getRSI(m.appConfig.SignalRSI, 0)

	// 超买超卖边界
	isOverbought := rsiVal > m.appConfig.SignalRSIOverBuy
	isOversold := rsiVal < m.appConfig.SignalRSIOverSell

	atr := m.GetATR(m.appConfig.SignalATR, 0)

	// ==================== 【金死叉核心算法判定降噪】 ====================
	// 引入动态噪声缓冲区，防止 DIF 与 DEA 贴合平行时微小波动的假交叉
	noiseThreshold := atr * m.appConfig.SignalATRNoiseThresholdFactor
	if noiseThreshold < m.appConfig.SignalATRNoiseThresholdMin {
		noiseThreshold = m.appConfig.SignalATRNoiseThresholdMin
	}

	// 经过降噪平滑后的真金叉、真死叉
	isRealDeathCross := rawDeathCross && (macdPrev.MACD-macdPrev.Signal > noiseThreshold)
	isRealGoldenCross := rawGoldenCross && (macdPrev.Signal-macdPrev.MACD > noiseThreshold)

	// 强多头/强空头区过滤：在零轴远端，实行假交叉豁免权
	if macdCurr.MACD > 0 && isRealDeathCross {
		// 零轴上方强多头区，若红柱子只是微幅震荡，抹除死叉信号，防止被洗下车
		if math.Abs(macdCurr.Histogram) < noiseThreshold {
			isRealDeathCross = false
		}
	}
	if macdCurr.MACD < 0 && isRealGoldenCross {
		// 零轴下方强空头区，若绿柱子只是微幅震荡，抹除金叉信号，防止空单早退
		if math.Abs(macdCurr.Histogram) < noiseThreshold {
			isRealGoldenCross = false
		}
	}

	// ==================== 【多头交易控制流】 ====================
	switch signalType {
	case types.SignalTrendLong:
		if isPriceUptrend {
			// 【多头顺势开仓】
			if (isRealGoldenCross || (macdCurr.Histogram > 0 && macdCurr.Histogram > macdPrev.Histogram)) && volSurge && !isOverbought {
				return types.SignalResult{
					Action:     types.BuyOrderAction, // 开仓做多
					Reason:     fmt.Sprintf("顺势做多: %d分趋势多头+MACD共振+放量突破", signalWindow),
					StopLoss:   currentPrice - (2.5 * atr),
					TakeProfit: currentPrice + (4.5 * atr),
				}
			}

			// 【多头波段止盈】
			if isRealDeathCross || isOverbought {
				return types.SignalResult{
					Action: types.SellOrderAction, // 平仓多头
					Reason: "多头波段止盈: 确认为右侧真实下破或超买",
				}
			}
		}

		// 多头硬性风控平仓线：转为暴跌下行趋势时，无条件斩仓多头避险
		if isPriceDowntrend && (rawDeathCross || isOversold) {
			return types.SignalResult{
				Action: types.SellOrderAction, // 强行平多
				Reason: "下行趋势风险控制: 多头强行平仓避险",
			}
		}
	case types.SignalTrendShort:
		if isPriceDowntrend {
			// 【空头顺势开仓】
			// 条件：处于下行趋势 + (真死叉确认 或 绿柱子在零轴下放大) + 放量共振 + 未超卖
			if (isRealDeathCross || (macdCurr.Histogram < 0 && macdCurr.Histogram < macdPrev.Histogram)) && volSurge && !isOversold {
				return types.SignalResult{
					Action:     types.SellOrderAction, // 开仓做空 (注意：在订单接收端Category应标记为Short)
					Reason:     fmt.Sprintf("顺势做空: %d分趋势空头+MACD死叉共振+放量下破", signalWindow),
					StopLoss:   currentPrice + (2.5 * atr),
					TakeProfit: currentPrice - (4.5 * atr),
				}
			}

			// 【空头波段止盈】
			if isRealGoldenCross || isOversold {
				return types.SignalResult{
					Action: types.BuyOrderAction, // 平仓空头 (回补)
					Reason: "空头波段止盈: 确认为右侧真实金叉回升或超卖",
				}
			}
		}

		// 空头硬性风控平仓线：转为暴涨上行趋势时，无条件斩仓空头避险
		if isPriceUptrend && (rawGoldenCross || isOverbought) {
			return types.SignalResult{
				Action: types.BuyOrderAction, // 强行平空
				Reason: "上行趋势风险控制: 空头强行平仓避险",
			}
		}
	}

	return types.SignalResult{Action: types.NoneOrderAction, Reason: "保持观望"}
}
