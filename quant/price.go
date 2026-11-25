package quant

import (
	"sync"

	"LongBridge-QuantSystem/config"
	"LongBridge-QuantSystem/logger"
	"LongBridge-QuantSystem/types"
	"LongBridge-QuantSystem/utils"

	"github.com/longportapp/openapi-go/quote"
)

// Price 价格更新器
type Price struct {
	mu          sync.RWMutex
	symbol      string
	appConfig   *config.AppConfig
	latestPrice float64 // 存储股票的最新价格

	secondHistory *SecondHistory
	minuteHistory *MinuteHistory
}

// NewPrice 创建价格更新器
func NewPrice(symbol string, appConfig *config.AppConfig) *Price {
	return &Price{
		symbol:      symbol,
		appConfig:   appConfig,
		latestPrice: 0,

		secondHistory: NewSecondHistory(appConfig, appConfig.SecondHistoryLimit),
		minuteHistory: NewMinuteHistory(appConfig, appConfig.MinuteHistoryLimit),
	}
}

// Clear 清除数据
func (m *Price) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latestPrice = 0

	m.secondHistory.Clear()
	m.minuteHistory.Clear()
}

// OnQuote 处理实时行情推送 - 存储价格历史
func (m *Price) OnQuote(ev *quote.PushQuote) {
	if ev == nil || ev.LastDone == nil {
		return
	}

	currentPrice, ok := utils.DecimalToFloat64(ev.LastDone)
	if !ok {
		logger.Warn("价格转换失败 %s: 原始值=%s", ev.Symbol, ev.LastDone.String())
		return
	}

	m.mu.Lock()
	m.latestPrice = currentPrice
	m.mu.Unlock()

	// 更新秒级历史
	m.secondHistory.Update(currentPrice, ev)

	// 更新分钟级历史
	m.minuteHistory.Update(currentPrice, ev)
}

func (m *Price) DrawMACD(filePath string, allOrders []*types.CompletedOrder) error {
	return m.minuteHistory.DrawMACD(filePath, allOrders)
}

func (m *Price) DrawPrice(filePath string, allOrders []*types.CompletedOrder) error {
	return m.minuteHistory.DrawPrice(filePath, allOrders)
}

func (m *Price) GetCurrentPrice() float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.latestPrice
}

func (m *Price) GetCurrentVolume(priceType types.PriceType, exNum int) float64 {
	switch priceType {
	case types.SecondType:
		return m.secondHistory.GetCurrentVolume(exNum)
	case types.MinuteType:
		return m.minuteHistory.GetCurrentVolume(exNum)
	default:
		return 0
	}
}

func (m *Price) GetAveragePrice(priceType types.PriceType, num int, exNum int) float64 {
	switch priceType {
	case types.SecondType:
		return m.secondHistory.GetAveragePrice(num, exNum)
	case types.MinuteType:
		return m.minuteHistory.GetAveragePrice(num, exNum)
	default:
		return 0
	}
}

func (m *Price) GetAverageVolume(priceType types.PriceType, num int, exNum int) float64 {
	switch priceType {
	case types.SecondType:
		return m.secondHistory.GetAverageVolume(num, exNum)
	case types.MinuteType:
		return m.minuteHistory.GetAverageVolume(num, exNum)
	default:
		return 0
	}
}

func (m *Price) GetPriceChangePercent(priceType types.PriceType, num int) float64 {
	switch priceType {
	case types.SecondType:
		return m.secondHistory.GetPriceChangePercent(num)
	case types.MinuteType:
		return m.minuteHistory.GetPriceChangePercent(num)
	default:
		return 0
	}
}

func (m *Price) GetHighPrice(priceType types.PriceType, num int) float64 {
	switch priceType {
	case types.SecondType:
		return m.secondHistory.GetHighPrice(num)
	case types.MinuteType:
		return m.minuteHistory.GetHighPrice(num)
	default:
		return 0
	}
}

func (m *Price) GetATR(priceType types.PriceType, num int, exNum int) float64 {
	switch priceType {
	case types.SecondType:
		return m.secondHistory.GetATR(num, exNum)
	case types.MinuteType:
		return m.minuteHistory.GetATR(num, exNum)
	default:
		return 0
	}
}

func (m *Price) GetRSI(priceType types.PriceType, num int, exNum int) float64 {
	switch priceType {
	case types.SecondType:
		return m.secondHistory.GetRSI(num, exNum)
	case types.MinuteType:
		return m.minuteHistory.GetRSI(num, exNum)
	default:
		return 50.0
	}
}

func (m *Price) GetMACD(priceType types.PriceType, num int) []*types.MACDData {
	switch priceType {
	case types.SecondType:
		return m.secondHistory.GetMACD(num)
	case types.MinuteType:
		return m.minuteHistory.GetMACD(num)
	default:
		return make([]*types.MACDData, 0)
	}
}

func (m *Price) GetMACDSignal(priceType types.PriceType) int {
	switch priceType {
	case types.SecondType:
		return m.secondHistory.GetMACDSignal()
	case types.MinuteType:
		return m.minuteHistory.GetMACDSignal()
	default:
		return 0
	}
}

func (m *Price) GetMACDStrength(priceType types.PriceType) float64 {
	switch priceType {
	case types.SecondType:
		return m.secondHistory.GetMACDStrength()
	case types.MinuteType:
		return m.minuteHistory.GetMACDStrength()
	default:
		return 0
	}
}

func (m *Price) GetSecondData(num int) []*types.SecondData {
	return m.secondHistory.GetData(num)
}

func (m *Price) CheckSignals() types.SignalResult {
	return m.minuteHistory.CheckSignals()
}
