package utils

import "math"

// CalculateEMA 计算EMA序列
func CalculateEMA(values []float64, period int) []float64 {
	if len(values) == 0 || period <= 0 {
		return []float64{}
	}
	ema := make([]float64, len(values))
	alpha := 2.0 / float64(period+1)
	for i, v := range values {
		if i == 0 {
			ema[i] = v
		} else {
			ema[i] = v*alpha + ema[i-1]*(1-alpha)
		}
	}
	return ema
}

// 预计算时间权重（线性映射到 [minWeight, maxWeight]）
// 参数 n: 窗口大小（数据条数）
// 返回: 长度为 n 的切片，索引0对应最旧数据，索引n-1对应最新数据，权重递增
func CalcTimeWeights(n int, minWeight, maxWeight float64) []float64 {
	if n <= 0 {
		return nil
	}
	if n == 1 {
		return []float64{maxWeight}
	}
	weights := make([]float64, n)
	step := (maxWeight - minWeight) / float64(n-1)
	for i := 0; i < n; i++ {
		weights[i] = minWeight + step*float64(i)
	}
	return weights
}

// CalculateADX 计算 ADX 指标
func CalculateADX(highs, lows, closes []float64, period int) float64 {
	if len(closes) < period*2+1 {
		return 0.0 // 数据不足以完成平滑
	}

	size := len(closes)
	tr := make([]float64, size)
	plusDM := make([]float64, size)
	minusDM := make([]float64, size)

	// 1. 计算 TR, +DM, -DM
	for i := 1; i < size; i++ {
		// TR = Max(H-L, |H-PC|, |L-PC|)
		hMinusL := highs[i] - lows[i]
		hMinusPC := math.Abs(highs[i] - closes[i-1])
		lMinusPC := math.Abs(lows[i] - closes[i-1])
		tr[i] = math.Max(hMinusL, math.Max(hMinusPC, lMinusPC))

		// DM
		upMove := highs[i] - highs[i-1]
		downMove := lows[i-1] - lows[i]

		if upMove > downMove && upMove > 0 {
			plusDM[i] = upMove
		} else {
			plusDM[i] = 0
		}

		if downMove > upMove && downMove > 0 {
			minusDM[i] = downMove
		} else {
			minusDM[i] = 0
		}
	}

	// 2. 平滑计算 (Wilder's Smoothing)
	var sumTR, sumPlusDM, sumMinusDM float64
	for i := 1; i <= period; i++ {
		sumTR += tr[i]
		sumPlusDM += plusDM[i]
		sumMinusDM += minusDM[i]
	}

	// 初始平滑值
	smoothTR := sumTR
	smoothPlusDM := sumPlusDM
	smoothMinusDM := sumMinusDM

	dxValues := make([]float64, size)

	// 计算 DX 序列
	for i := period + 1; i < size; i++ {
		// 持续平滑公式
		smoothTR = smoothTR - (smoothTR / float64(period)) + tr[i]
		smoothPlusDM = smoothPlusDM - (smoothPlusDM / float64(period)) + plusDM[i]
		smoothMinusDM = smoothMinusDM - (smoothMinusDM / float64(period)) + minusDM[i]

		if smoothTR == 0 {
			dxValues[i] = 0
			continue
		}

		plusDI := 100 * smoothPlusDM / smoothTR
		minusDI := 100 * smoothMinusDM / smoothTR

		div := math.Abs(plusDI + minusDI)
		if div == 0 {
			dxValues[i] = 0
		} else {
			dxValues[i] = 100 * math.Abs(plusDI-minusDI) / div
		}
	}

	// 3. 计算最终的 ADX (对 DX 进行再一次平滑)
	var sumDX float64
	// 找到第一个有效的 DX 开始平滑
	startIdx := period + 1
	for i := startIdx; i < startIdx+period; i++ {
		sumDX += dxValues[i]
	}

	adx := sumDX / float64(period)
	for i := startIdx + period; i < size; i++ {
		adx = (adx*float64(period-1) + dxValues[i]) / float64(period)
	}

	return adx
}
