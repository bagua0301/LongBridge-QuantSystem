package strategy_t

import (
	"LongBridge-QuantSystem/logger"
	"LongBridge-QuantSystem/utils"
	"math"
	"time"

	"github.com/longportapp/openapi-go/quote"
)

// TestPriceStrategy 测试网格策略
type TestPriceStrategy struct {
	name        string
	description string

	priceTicker        *time.Ticker
	quotes             []*quote.PushQuote
	logicInitTimestamp int64
	timeScale          float64 // 时间缩放因子，例如 10 表示 1秒模拟 10秒
	priceIndex         int
	isInit             bool
	stopChan           chan struct{}
}

// NewTestPriceStrategy 创建测试价格策略
func NewTestPriceStrategy() *TestPriceStrategy {
	return &TestPriceStrategy{
		name:               "price",
		description:        "测试网格策略：推送价格",
		logicInitTimestamp: 0,
		timeScale:          10,
		priceIndex:         0,
		isInit:             false,
		stopChan:           make(chan struct{}),
	}
}

// GetName 获取策略名称
func (s *TestPriceStrategy) GetName() string {
	return s.name
}

// GetDescription 获取策略描述
func (s *TestPriceStrategy) GetDescription() string {
	return s.description
}

// GetRequiredData 获取需要的数据类型
func (s *TestPriceStrategy) GetRequiredData() []string {
	return []string{"quote"}
}

// Init 初始化策略
func (s *TestPriceStrategy) Init() {
	s.isInit = true
	if GlobalTestManager.appConfig.TestTimeScale == 0 {
		logger.Fatal("TestTimeScale == 0")
		return
	}
	s.timeScale = GlobalTestManager.appConfig.TestTimeScale
	// 创建Excel读取器
	reader := utils.NewExcelQuoteReader(GlobalTestManager.appConfig.TestXlsx)

	// 读取所有行情数据
	quotes, err := reader.ReadQuotes("Quotes")
	if err != nil {
		logger.Fatal("读取Excel失败: %v", err)
		return
	}
	s.quotes = quotes
	s.priceIndex = GlobalTestManager.appConfig.TestPriceIndexStart
	logger.Warn("初始化完成，价格序列长度: %d, timeScale=%f, TestPriceIndexStart=%d, TestPriceIndexEnd=%d",
		len(s.quotes), s.timeScale, GlobalTestManager.appConfig.TestPriceIndexStart, GlobalTestManager.appConfig.TestPriceIndexEnd)
}

// Run 运行策略
func (s *TestPriceStrategy) Run() {
	if !s.isInit {
		logger.Error("未初始化，请先调用 Init()")
		return
	}

	if GlobalTestManager.appConfig.TestTimeScale <= 1.0 {
		s.priceTicker = time.NewTicker(time.Duration(GlobalTestManager.appConfig.TestTimeTicker) * time.Second)
		go func() {
			for {
				select {
				case <-s.stopChan:
					return
				case <-s.priceTicker.C:
					if !s.End() {
						s.PushNextPrice()
					} else {
						logger.Info("价格序列已结束，停止推送")
						s.StopPricePushing()
						return
					}
				}
			}
		}()
	} else {
		go s.PushNextPrice()
	}
}

// PushNextPrice 推送下一个价格
func (s *TestPriceStrategy) PushNextPrice() {
	dataLen := len(s.quotes)
	if dataLen <= 0 {
		logger.Warn("dataLen <=0,停止推送")
		s.StopPricePushing()
		return
	}
	if s.priceIndex >= dataLen {
		logger.Warn("价格序列推送完成,停止推送")
		s.StopPricePushing()
		return
	}

	// 先设置虚拟时间,让订单行情符合真实交易时间
	if s.logicInitTimestamp == 0 {
		index := s.priceIndex
		if index < len(s.quotes) {
			utils.VirtualClockSet(utils.Float64ToTime(s.quotes[index].Timestamp))
		} else {
			logger.Fatal("index=%d, len(s.quotes)=%d", index, len(s.quotes))
			return
		}
	}
	var curTime int64 = 0
	if GlobalTestManager.appConfig.TestTimeScale <= 1.0 {
		curTime = time.Now().Unix()
	} else {
		curTime = utils.Now().Unix()
	}
	if s.logicInitTimestamp == 0 {
		index := s.priceIndex
		// 正序
		if GlobalTestManager.appConfig.TestPriceIndexType == 0 {
			if index < dataLen {
				// 取第1个时间
				firstTime := s.quotes[index].Timestamp
				for i := index; i < dataLen; i++ {
					data := s.quotes[i]
					data.Timestamp = curTime + (data.Timestamp - firstTime)
				}
			}
			s.logicInitTimestamp = curTime

			logger.Warn("预计%d秒后结束", (s.quotes[dataLen-1].Timestamp-curTime)/int64(s.timeScale))
		} else {
			// 取最后1个时间
			firstTime := s.quotes[dataLen-1].Timestamp
			for k, v := range s.quotes {
				if k == dataLen-1 {
					v.Volume = 0
				} else {
					v.Volume = s.quotes[k+1].Volume - v.Volume
				}
				v.Timestamp = curTime + int64(math.Abs(float64(v.Timestamp-firstTime)))
			}
			s.logicInitTimestamp = curTime

			logger.Warn("预计%d秒后结束", (s.quotes[0].Timestamp-curTime)/int64(s.timeScale))
		}
	}
	// 使用时间缩放因子计算虚拟时间
	elapsedVirtual := curTime - s.logicInitTimestamp

	pushData := make([]*quote.PushQuote, 0)
	if GlobalTestManager.appConfig.TestTimeScale <= 1.0 {
		// 正序
		if GlobalTestManager.appConfig.TestPriceIndexType == 0 {
			for i := s.priceIndex; i < len(s.quotes); i++ {
				data := s.quotes[i]
				if data.Timestamp <= curTime+elapsedVirtual {
					pushData = append(pushData, data)
					s.priceIndex++
				} else {
					break
				}
			}
		} else {
			for i := len(s.quotes) - s.priceIndex - 1; i >= 0; i-- {
				data := s.quotes[i]
				if data.Timestamp <= curTime+elapsedVirtual {
					pushData = append(pushData, data)
					s.priceIndex++
				} else {
					break
				}
			}
		}
	} else {
		// 正序
		if GlobalTestManager.appConfig.TestPriceIndexType == 0 {
			pushData = s.quotes
			s.priceIndex = len(s.quotes)
		} else {
			for i := len(s.quotes) - s.priceIndex - 1; i >= 0; i-- {
				pushData = append(pushData, s.quotes[i])
				s.priceIndex++
			}
		}
	}
	s.PushPrice(pushData)
}

// PushPrice 推送单个价格
func (s *TestPriceStrategy) PushPrice(data []*quote.PushQuote) {
	if len(data) <= 0 {
		return
	}
	if GlobalTestManager.appConfig.TestPriceIndexType == 0 {
		logger.Info("正序推送价格: 第%d步", s.priceIndex)
	} else {
		logger.Info("逆序推送价格: 第%d步", s.priceIndex)
	}
	// 使用全局测试管理器
	if GetGlobalTestManager() != nil {
		GetGlobalTestManager().handlePriceEvent(data)
	}
	// 直接结束
	if GlobalTestManager.appConfig.TestTimeScale > 1.0 {
		s.StopPricePushing()
	}
}

// StopPricePushing 停止价格推送
func (s *TestPriceStrategy) StopPricePushing() {
	if s.priceTicker != nil {
		s.priceTicker.Stop()
	}
	close(s.stopChan)
	// 使用全局测试管理器
	if GetGlobalTestManager() != nil {
		GetGlobalTestManager().Stop()
	}
}

// End 检查是否结束
func (s *TestPriceStrategy) End() bool {
	return s.isInit && (s.priceIndex >= len(s.quotes) ||
		(GlobalTestManager.appConfig.TestPriceIndexEnd > 0 &&
			s.priceIndex >= GlobalTestManager.appConfig.TestPriceIndexEnd))
}

// GetCurrentState 获取当前状态
func (s *TestPriceStrategy) GetCurrentState() map[string]interface{} {
	return map[string]interface{}{
		"is_init":      s.isInit,
		"price_index":  s.priceIndex,
		"total_prices": len(s.quotes),
		"is_running":   s.priceTicker != nil,
	}
}
