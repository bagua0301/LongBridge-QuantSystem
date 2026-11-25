package quant

import (
	"context"
	"strconv"

	myconfig "LongBridge-QuantSystem/config"
	"LongBridge-QuantSystem/logger"
	"LongBridge-QuantSystem/types"
)

// loadInitialPositions 加载初始持仓
func (qs *QuantSystem) loadInitialPositions(ctx context.Context) {
	symbols := qs.AppConfig.Symbols
	if len(symbols) == 0 {
		symbols = []string{}
	}

	stockPositions, err := qs.TradeCtx.StockPositions(ctx, symbols)
	if err != nil {
		logger.Warn("获取股票持仓失败: %v", err)
		return
	}

	qs.PositionsLock.Lock()
	defer qs.PositionsLock.Unlock()

	totalPositions := 0

	for _, channel := range stockPositions {
		if channel == nil || len(channel.Positions) == 0 {
			continue
		}
		for _, pos := range channel.Positions {
			if pos == nil {
				continue
			}
			var quantity float64
			if pos.Quantity != "" {
				qty, err := strconv.ParseFloat(pos.Quantity, 64)
				if err == nil {
					quantity = qty
				}
			}
			if quantity <= 0 {
				continue
			}
			var costPrice float64
			if pos.CostPrice != nil {
				costPrice, _ = pos.CostPrice.Float64()
			}
			var currentPrice float64
			if qs.PriceManager != nil {
				currentPrice = qs.PriceManager.GetCurrentPrice(pos.Symbol)
			}
			marketValue := quantity * currentPrice
			totalCost := quantity * costPrice
			profitLoss := marketValue - totalCost
			var profitRate float64
			if costPrice > 0 {
				profitRate = (currentPrice - costPrice) / costPrice * 100
			}
			qs.Positions[pos.Symbol] = &types.PositionInfo{
				Symbol:       pos.Symbol,
				Quantity:     quantity,
				AvgPrice:     costPrice,
				CurrentPrice: currentPrice,
				MarketValue:  marketValue,
				ProfitLoss:   profitLoss,
				ProfitRate:   profitRate,
				StrategyName: "",
			}
			totalPositions++
			logger.Info("初始持仓: %s (%s), 数量: %.2f, 成本: %.4f, 当前: %.4f, 市值: %.2f, 盈亏: %.2f (%+.2f%%)",
				pos.Symbol, pos.SymbolName, quantity, costPrice, currentPrice, marketValue, profitLoss, profitRate)
		}
	}
	logger.Info("初始持仓加载完成，共 %d 只股票", totalPositions)
}

// loadInitialFundPositions 加载初始基金持仓
func (qs *QuantSystem) loadInitialFundPositions(ctx context.Context) {
	channels, err := qs.TradeCtx.FundPositions(ctx, qs.AppConfig.Symbols)
	if err != nil {
		logger.Warn("获取基金持仓失败: %v", err)
		return
	}
	qs.PositionsLock.Lock()
	defer qs.PositionsLock.Unlock()
	totalPositions := 0
	for _, channel := range channels {
		if channel == nil || len(channel.Positions) == 0 {
			continue
		}
		for _, pos := range channel.Positions {
			if pos == nil {
				continue
			}
			var holdingUnits float64
			if pos.HoldingUnits != nil {
				holdingUnits, _ = pos.HoldingUnits.Float64()
			}
			if holdingUnits <= 0 {
				continue
			}
			var costNetAssetValue float64
			if pos.CostNetAssetValue != nil {
				costNetAssetValue, _ = pos.CostNetAssetValue.Float64()
			}
			var currentNetAssetValue float64
			if pos.CurrentNetAssetValue != nil {
				currentNetAssetValue, _ = pos.CurrentNetAssetValue.Float64()
			}
			marketValue := holdingUnits * currentNetAssetValue
			totalCost := holdingUnits * costNetAssetValue
			profitLoss := marketValue - totalCost
			var profitRate float64
			if costNetAssetValue > 0 {
				profitRate = (currentNetAssetValue - costNetAssetValue) / costNetAssetValue * 100
			}
			qs.Positions[pos.Symbol] = &types.PositionInfo{
				Symbol:       pos.Symbol,
				Quantity:     holdingUnits,
				AvgPrice:     costNetAssetValue,
				CurrentPrice: currentNetAssetValue,
				MarketValue:  marketValue,
				ProfitLoss:   profitLoss,
				ProfitRate:   profitRate,
				StrategyName: "",
			}
			totalPositions++
			logger.Info("初始基金持仓: %s (%s), 数量: %.2f, 成本: %.4f, 当前: %.4f, 市值: %.2f, 盈亏: %.2f (%+.2f%%)",
				pos.Symbol, pos.SymbolName, holdingUnits, costNetAssetValue, currentNetAssetValue, marketValue, profitLoss, profitRate)
		}
	}
	logger.Info("初始基金持仓加载完成，共 %d 只", totalPositions)
}

// GetPositions 获取所有持仓
func (qs *QuantSystem) GetPositions() map[string]*types.PositionInfo {
	qs.PositionsLock.RLock()
	defer qs.PositionsLock.RUnlock()

	result := make(map[string]*types.PositionInfo)
	for k, v := range qs.Positions {
		result[k] = v
	}
	return result
}

// GetPosition 获取单个持仓
func (qs *QuantSystem) GetPosition(symbol string) *types.PositionInfo {
	qs.PositionsLock.RLock()
	defer qs.PositionsLock.RUnlock()

	if pos, ok := qs.Positions[symbol]; ok {
		return pos
	}
	return nil
}

// getAverageCost 获取股票的平均成本（内部方法）
func (qs *QuantSystem) GetAverageCost(symbol string) float64 {
	qs.PositionsLock.RLock()
	defer qs.PositionsLock.RUnlock()

	if pos, exists := qs.Positions[symbol]; exists {
		return pos.AvgPrice
	}
	return 0
}

// UpdatePositionFromOrder 根据成交订单更新持仓
func (qs *QuantSystem) UpdatePositionFromOrder(order *types.CompletedOrder) {
	qs.PositionsLock.Lock()
	defer qs.PositionsLock.Unlock()

	logger.Debug("更新持仓: %s, 操作=%s, 数量=%.2f, 价格=%.4f, 策略ID=%s",
		order.Symbol, order.Action, order.Quantity, order.Price, order.StrategyName)

	pos, exists := qs.Positions[order.Symbol]
	if !exists {
		pos = &types.PositionInfo{
			Symbol:       order.Symbol,
			StrategyName: order.StrategyName,
			Category:     order.Category,
		}
		qs.Positions[order.Symbol] = pos
	}

	if order.Category == types.LongOrderCategory {
		if order.Action == types.BuyOrderAction {
			// 买入：增加持仓
			totalCost := pos.Quantity*pos.AvgPrice + order.Amount
			pos.Quantity += order.Quantity
			if pos.Quantity > 0 {
				pos.AvgPrice = totalCost / pos.Quantity
			}
			pos.CurrentPrice = order.Price
			pos.MarketValue = pos.Quantity * pos.CurrentPrice
			logger.Info("做多买入更新持仓: %s, 数量 +%.0f = %.2f, 新成本: %.4f [策略:%s]",
				order.Symbol, order.Quantity, pos.Quantity, pos.AvgPrice, pos.StrategyName)
		} else {
			// 卖出：减少持仓
			if pos.Quantity < order.Quantity {
				logger.Error("做多卖出数量超过持仓: %s, 持有 %.2f, 卖出 %.0f",
					order.Symbol, pos.Quantity, order.Quantity)
				pos.Quantity = 0
			} else {
				pos.Quantity -= order.Quantity
			}

			if pos.Quantity <= 0 {
				logger.Info("做多卖出更新持仓: %.2f, 完成: %s, 清仓 [策略:%s]", pos.Quantity, order.Symbol, pos.StrategyName)
				delete(qs.Positions, order.Symbol)
			} else {
				pos.CurrentPrice = order.Price
				pos.MarketValue = pos.Quantity * pos.CurrentPrice
				pos.ProfitLoss = pos.MarketValue - pos.Quantity*pos.AvgPrice
				if pos.AvgPrice > 0 {
					pos.ProfitRate = (pos.CurrentPrice - pos.AvgPrice) / pos.AvgPrice * 100
				}
				logger.Info("做多卖出更新持仓: %s, 数量 -%.0f = %.2f, 浮动盈亏: %.2f [策略:%s]",
					order.Symbol, order.Quantity, pos.Quantity, pos.ProfitLoss, pos.StrategyName)
			}
		}
	} else {
		if order.Action == types.SellOrderAction {
			// 做空卖出：增加持仓
			totalCost := pos.Quantity*pos.AvgPrice + order.Amount
			pos.Quantity += order.Quantity
			if pos.Quantity > 0 {
				pos.AvgPrice = totalCost / pos.Quantity
			}
			pos.CurrentPrice = order.Price
			pos.MarketValue = pos.Quantity * pos.CurrentPrice
			logger.Info("做空卖出更新持仓: %s, 数量 +%.0f = %.2f, 新成本: %.4f [策略:%s]",
				order.Symbol, order.Quantity, pos.Quantity, pos.AvgPrice, pos.StrategyName)
		} else {
			// 做空买入：减少持仓
			if pos.Quantity < order.Quantity {
				logger.Error("做空买入数量超过持仓: %s, 持有 %.2f, 卖出 %.0f",
					order.Symbol, pos.Quantity, order.Quantity)
				pos.Quantity = 0
			} else {
				pos.Quantity -= order.Quantity
			}

			if pos.Quantity <= 0 {
				logger.Info("做空买入更新持仓: %.2f, 完成: %s, 清仓 [策略:%s]", pos.Quantity, order.Symbol, pos.StrategyName)
				delete(qs.Positions, order.Symbol)
			} else {
				pos.CurrentPrice = order.Price
				pos.MarketValue = pos.Quantity * pos.CurrentPrice
				pos.ProfitLoss = pos.Quantity*pos.AvgPrice - pos.MarketValue
				if pos.AvgPrice > 0 {
					pos.ProfitRate = (pos.AvgPrice - pos.CurrentPrice) / pos.AvgPrice * 100
				}
				logger.Info("做空买入更新持仓: %s, 数量 -%.0f = %.2f, 浮动盈亏: %.2f [策略:%s]",
					order.Symbol, order.Quantity, pos.Quantity, pos.ProfitLoss, pos.StrategyName)
			}
		}
	}

}

// UpdatePositionsPricesFromCache 从缓存更新持仓价格
func (qs *QuantSystem) UpdatePositionsPricesFromCache() {
	qs.PositionsLock.Lock()
	defer qs.PositionsLock.Unlock()

	for symbol, pos := range qs.Positions {
		if pos == nil {
			logger.Debug("持仓为空: %s", symbol)
			continue
		}

		if qs.PriceManager == nil {
			logger.Debug("价格更新器为空")
			continue
		}

		currentPrice := qs.PriceManager.GetCurrentPrice(symbol)
		pos.CurrentPrice = currentPrice
		pos.MarketValue = pos.Quantity * currentPrice
		if pos.Category == types.LongOrderCategory {
			pos.ProfitLoss = pos.MarketValue - pos.Quantity*pos.AvgPrice
			if pos.AvgPrice > 0 {
				pos.ProfitRate = (currentPrice - pos.AvgPrice) / pos.AvgPrice * 100
			}
		} else {
			pos.ProfitLoss = pos.Quantity*pos.AvgPrice - pos.MarketValue
			if pos.AvgPrice > 0 {
				pos.ProfitRate = (pos.AvgPrice - currentPrice) / pos.AvgPrice * 100
			}
		}
	}
}

// CheckStopLoss 检查止损
func (qs *QuantSystem) CheckStopLoss() {
	qs.PositionsLock.RLock()
	defer qs.PositionsLock.RUnlock()

	for _, pos := range qs.Positions {
		if pos.CurrentPrice <= 0 {
			continue
		}
		if pos.Quantity > 0 && pos.ProfitRate <= -qs.AppConfig.StopLossPercent {
			logger.Warn("触发止损: %s, 亏损率: %.2f%%, 当前价: %.2f, 成本: %.2f [策略:%s]",
				pos.Symbol, pos.ProfitRate, pos.CurrentPrice, pos.AvgPrice, pos.StrategyName)

			qs.StrategyMgr.SetCmdSell(pos.Symbol, "all")
		}
	}
}

// CheckMaxPosition 检查最大持仓比例
func (qs *QuantSystem) CheckMaxPosition(symbol string, orderQuantity int64, price float64) bool {
	if !myconfig.IsTestMode() {
		return true
	}

	qs.PositionsLock.RLock()
	defer qs.PositionsLock.RUnlock()

	totalValue := qs.AppConfig.BaseAmount

	// 只计算当前策略的持仓市值
	var currentValue float64
	if pos, exists := qs.Positions[symbol]; exists {
		currentValue = pos.MarketValue
	}

	newValue := currentValue + float64(orderQuantity)*price
	newPercent := (newValue / totalValue) * 100

	if newPercent > qs.AppConfig.MaxPositionPercent {
		logger.Warn("持仓比例超限: %s, 当前 %.2f%%, 目标 %.2f%%, 限制 %.2f%%",
			symbol, currentValue/totalValue*100, newPercent, qs.AppConfig.MaxPositionPercent)
		return false
	}
	return true
}
