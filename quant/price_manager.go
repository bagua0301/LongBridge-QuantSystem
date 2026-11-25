package quant

import (
	"LongBridge-QuantSystem/config"
	"LongBridge-QuantSystem/logger"
	"LongBridge-QuantSystem/types"
	"sync"

	"github.com/longportapp/openapi-go/quote"
)

type priceManagerImpl struct {
	mu        sync.RWMutex
	prices    map[string]*Price
	appConfig *config.AppConfig
}

func NewPriceManager(appConfig *config.AppConfig) types.PriceManagerInterface {
	impl := &priceManagerImpl{
		prices:    make(map[string]*Price),
		appConfig: appConfig,
	}
	for _, v := range appConfig.Symbols {
		impl.Create(v)
	}
	return impl
}

func (m *priceManagerImpl) Create(symbol string) *Price {
	m.mu.RLock()
	p, ok := m.prices[symbol]
	m.mu.RUnlock()
	if ok {
		return p
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok = m.prices[symbol]; ok {
		return p
	}
	p = NewPrice(symbol, m.appConfig)
	m.prices[symbol] = p
	return p
}

func (m *priceManagerImpl) Get(symbol string) *Price {
	m.mu.RLock()
	p, ok := m.prices[symbol]
	m.mu.RUnlock()
	if !ok {
		return nil
	}
	return p
}

func (m *priceManagerImpl) OnQuote(ev *quote.PushQuote) {
	if ev == nil || ev.Symbol == "" {
		return
	}
	p := m.Get(ev.Symbol)
	if p == nil {
		return
	}
	logger.Debug("行情推送 %s ev:%v", ev.Symbol, ev)
	p.OnQuote(ev)
}

func (m *priceManagerImpl) GetAllSymbol() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	symbols := make([]string, 0, len(m.prices))
	for s := range m.prices {
		symbols = append(symbols, s)
	}
	return symbols
}

func (m *priceManagerImpl) ClearSymbol(symbol string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.prices, symbol)
}

func (m *priceManagerImpl) ClearAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prices = make(map[string]*Price)
}

func (m *priceManagerImpl) DrawMACD(symbol string, filePath string, allOrders []*types.CompletedOrder) error {
	p := m.Get(symbol)
	if p == nil {
		logger.Error("p == nil symbol=%s", symbol)
		return nil
	}
	return p.DrawMACD(filePath, allOrders)
}

func (m *priceManagerImpl) DrawPrice(symbol string, filePath string, allOrders []*types.CompletedOrder) error {
	p := m.Get(symbol)
	if p == nil {
		logger.Error("p == nil symbol=%s", symbol)
		return nil
	}
	return p.DrawPrice(filePath, allOrders)
}

func (m *priceManagerImpl) GetCurrentPrice(symbol string) float64 {
	p := m.Get(symbol)
	if p == nil {
		return 0
	}
	return p.GetCurrentPrice()
}

func (m *priceManagerImpl) GetCurrentVolume(symbol string, priceType types.PriceType, exNum int) float64 {
	p := m.Get(symbol)
	if p == nil {
		return 0
	}
	return p.GetCurrentVolume(priceType, exNum)
}

func (m *priceManagerImpl) GetAveragePrice(symbol string, priceType types.PriceType, num int, exNum int) float64 {
	p := m.Get(symbol)
	if p == nil {
		return 0
	}
	return p.GetAveragePrice(priceType, num, exNum)
}

func (m *priceManagerImpl) GetAverageVolume(symbol string, priceType types.PriceType, num int, exNum int) float64 {
	p := m.Get(symbol)
	if p == nil {
		return 0
	}
	return p.GetAverageVolume(priceType, num, exNum)
}

func (m *priceManagerImpl) GetATR(symbol string, priceType types.PriceType, num int, exNum int) float64 {
	p := m.Get(symbol)
	if p == nil {
		return 0
	}
	return p.GetATR(priceType, num, exNum)
}

func (m *priceManagerImpl) GetRSI(symbol string, priceType types.PriceType, num int, exNum int) float64 {
	p := m.Get(symbol)
	if p == nil {
		return 0
	}
	return p.GetRSI(priceType, num, exNum)
}

func (m *priceManagerImpl) GetMACD(symbol string, priceType types.PriceType, num int) []*types.MACDData {
	p := m.Get(symbol)
	if p == nil {
		return make([]*types.MACDData, 0)
	}
	return p.GetMACD(priceType, num)
}

func (m *priceManagerImpl) GetMACDSignal(symbol string, priceType types.PriceType) int {
	p := m.Get(symbol)
	if p == nil {
		return 0
	}
	return p.GetMACDSignal(priceType)
}

func (m *priceManagerImpl) GetMACDStrength(symbol string, priceType types.PriceType) float64 {
	p := m.Get(symbol)
	if p == nil {
		return 0
	}
	return p.GetMACDStrength(priceType)
}

func (m *priceManagerImpl) GetSecondData(symbol string, num int) []*types.SecondData {
	p := m.Get(symbol)
	if p == nil {
		return []*types.SecondData{}
	}
	return p.GetSecondData(num)
}

func (m *priceManagerImpl) CheckSignals(symbol string) types.SignalResult {
	p := m.Get(symbol)
	if p == nil {
		return types.SignalResult{}
	}
	return p.CheckSignals()
}
