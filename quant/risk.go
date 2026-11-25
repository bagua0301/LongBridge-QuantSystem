package quant

import (
	"context"
	"time"

	myconfig "LongBridge-QuantSystem/config"
	"LongBridge-QuantSystem/utils"

	"LongBridge-QuantSystem/logger"
)

// mainLoop 主循环
func (qs *QuantSystem) mainLoop(ctx context.Context) {
	logger.Warn("主循环启动，开始监控策略信号...")
	logger.Warn("监控股票: %v", qs.AppConfig.Symbols)

	ticker := time.NewTicker(time.Duration(qs.AppConfig.OrderCheckTime) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !qs.IsRunning() {
				return
			}
			// 夜盘没有数据自动退出
			if !myconfig.IsTestMode() && qs.StrategyMgr.CanExit() {
				qs.StrategyMgr.Stop()
			}
			// 检查待确认订单状态
			qs.OrderQuery.CheckPendingOrders(ctx)
		}
	}
}

func (qs *QuantSystem) OrderLogic() {
	if !qs.IsRunning() {
		return
	}

	// 检查每日交易次数限制
	if !qs.checkDailyTradeLimit() {
		logger.Warn("已达到当日最大交易次数 (%d)，暂停交易", qs.AppConfig.MaxDailyTrades)

		// 输出统计报告
		qs.printAllSummary()
		qs.Stop()
		logger.Warn("程序将在5秒后退出...")
		time.Sleep(5 * time.Second)
		return
	}

	if !myconfig.IsTestMode() {
		info := qs.TradeControl.GetCurrentTimeInfo()
		logger.Debug("时段: %s, 允许交易: %v", info["session"], info["is_allowed"])

		if !qs.TradeControl.IsAllowed() {
			return
		}
		logger.Debug("当前时段允许交易，开始执行策略...")
	} else {
		logger.Debug("测试模式允许交易，开始执行策略...")
	}

	// 更新持仓价格
	qs.UpdatePositionsPricesFromCache()

	// 检查止损
	qs.CheckStopLoss()

	allSymbol := qs.PriceManager.GetAllSymbol()
	for _, symbol := range allSymbol {
		// 分析并生成订单
		orders, err := qs.StrategyMgr.GetSignal(symbol)
		if err != nil {
			logger.Info("err:%v", err)
			continue
		}

		if len(orders) > 0 {
			logger.Info("生成了 %d 个订单", len(orders))
		}

		// 执行订单
		for _, order := range orders {
			if !qs.checkDailyTradeLimit() {
				logger.Warn("达到交易次数限制，停止执行剩余订单")
				return
			}

			if order.Symbol == "" || order.Quantity <= 0 || order.TargetPrice <= 0 {
				logger.Warn("订单无效: %s, 数量=%d, 价格=%.4f", order.Symbol, order.Quantity, order.TargetPrice)
				continue
			}

			qs.executeOrderWithRisk(&order)
		}
	}
}

// GetDailyTradeCount 获取每日交易次数（实现接口）
func (qs *QuantSystem) GetDailyTradeCount() int {
	qs.TradeLock.Lock()
	defer qs.TradeLock.Unlock()
	return qs.DailyTradeCount
}

// GetLastTradeDate 获取最后交易日期（实现接口）
func (qs *QuantSystem) GetLastTradeDate() string {
	qs.TradeLock.Lock()
	defer qs.TradeLock.Unlock()
	return qs.LastTradeDate
}

// ResetDailyTradeCount 重置每日交易计数（实现接口）
func (qs *QuantSystem) ResetDailyTradeCount() {
	qs.TradeLock.Lock()
	defer qs.TradeLock.Unlock()
	qs.DailyTradeCount = 0
	qs.LastTradeDate = utils.Now().Format("2006-01-02")
}

// checkDailyTradeLimit 检查每日交易次数限制
func (qs *QuantSystem) checkDailyTradeLimit() bool {
	qs.TradeLock.Lock()
	defer qs.TradeLock.Unlock()

	today := utils.Now().Format("2006-01-02")
	if qs.LastTradeDate != today {
		qs.DailyTradeCount = 0
		qs.LastTradeDate = today
	}
	return qs.DailyTradeCount < qs.AppConfig.MaxDailyTrades
}

// IncrementDailyTradeCount 增加交易计数
func (qs *QuantSystem) IncrementDailyTradeCount() {
	qs.TradeLock.Lock()
	defer qs.TradeLock.Unlock()
	qs.DailyTradeCount++
	logger.Info("今日已交易次数: %d/%d", qs.DailyTradeCount, qs.AppConfig.MaxDailyTrades)
}
