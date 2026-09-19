package model

import (
	"math"
	"testing"
)

// ── RoundCredits：收敛到业务精度 ──

func TestRoundCreditsCleansFloatNoise(t *testing.T) {
	// 浮点累加的典型产物，必须收敛干净：否则内存值与落库值不一致
	if got := RoundCredits(0.001 + 0.001 + 0.001); got != 0.003 {
		t.Errorf("RoundCredits(3×0.001) = %v, want 0.003", got)
	}
	if got := RoundCredits(0.1 + 0.2); got != 0.3 {
		t.Errorf("RoundCredits(0.1+0.2) = %v, want 0.3", got)
	}
}

func TestRoundCreditsRoundsToBusinessPrecision(t *testing.T) {
	cases := []struct {
		in   float64
		want float64
	}{
		{1, 1},
		{1.5, 1.5},
		{0.001, 0.001},
		{0.0014, 0.001}, // 第 4 位舍去
		{0.0015, 0.002}, // 第 4 位进位
		{123.4567, 123.457},
		{0, 0},
	}

	for _, tc := range cases {
		if got := RoundCredits(tc.in); got != tc.want {
			t.Errorf("RoundCredits(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// 幂等：收敛过的值再收敛一次不变，否则「写库前收敛」和「读回来再收敛」会漂移
func TestRoundCreditsIsIdempotent(t *testing.T) {
	for _, v := range []float64{0.001, 1.0 / 3.0, 2.0 / 7.0, 123.456789} {
		once := RoundCredits(v)
		if twice := RoundCredits(once); twice != once {
			t.Errorf("RoundCredits 不幂等: %v -> %v -> %v", v, once, twice)
		}
	}
}

// ── ValidCreditPrecision：录入位数的守卫 ──

func TestValidCreditPrecision(t *testing.T) {
	cases := []struct {
		name string
		in   float64
		want bool
	}{
		{"整数", 1, true},
		{"两位", 1.25, true},
		{"恰好业务精度", 0.001, true},
		{"超出业务精度", 0.0001, false},
		{"多一位", 1.0001, false},
		{"零", 0, true},
		{"浮点噪声不算超位", 0.1 + 0.2, true},
		{"两位小数之积", 0.01 * 3, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidCreditPrecision(tc.in); got != tc.want {
				t.Errorf("ValidCreditPrecision(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// 与 RoundCredits 互为配套：收敛后的值必然通过校验。
// 若这条不成立，计费结果会被自己的录入校验拒掉。
func TestValidCreditPrecisionAcceptsRoundedValues(t *testing.T) {
	for _, v := range []float64{0.001 + 0.001, 0.1 + 0.2, 1.0 / 3.0, 2.0 / 7.0, 999.9999} {
		if rounded := RoundCredits(v); !ValidCreditPrecision(rounded) {
			t.Errorf("RoundCredits(%v) = %v 未通过 ValidCreditPrecision", v, rounded)
		}
	}
}

// 存储标度必须严格大于业务精度：列比业务口径窄时数据库会替我们舍入，
// 「内存值与实际扣费不一致」就会回来。
func TestStorageScaleExceedsBusinessPrecision(t *testing.T) {
	if CreditStorageScale <= CreditPrecision {
		t.Fatalf("存储标度 %d 必须大于业务精度 %d", CreditStorageScale, CreditPrecision)
	}
}

// ── 账本边界：NormalizeDeductAmount ──

func TestNormalizeDeductAmountCleansNoise(t *testing.T) {
	got, err := NormalizeDeductAmount(0.001 + 0.001 + 0.001)
	if err != nil {
		t.Fatalf("正常金额不该报错: %v", err)
	}
	if got != 0.003 {
		t.Errorf("收敛结果 = %v, want 0.003", got)
	}
}

// 负数扣费在 SQL 里是 credits - (-x)，会反向给用户加积分——必须挡在账本边界
func TestNormalizeDeductAmountRejectsNegative(t *testing.T) {
	if _, err := NormalizeDeductAmount(-0.001); err == nil {
		t.Error("负数扣费应被拒绝")
	}
}

// 低于最小单位的正数收敛成 0，静默放行等于白送服务
func TestNormalizeDeductAmountRejectsPositiveRoundingToZero(t *testing.T) {
	for _, v := range []float64{0.0001, 0.0004, 0.0004999} {
		if _, err := NormalizeDeductAmount(v); err == nil {
			t.Errorf("%v 低于最小单位却未报错，会变成免费", v)
		}
	}
}

// 免费模型是受支持的配置（0 积分的规则），不能一刀切拒绝
func TestNormalizeDeductAmountAllowsExactZero(t *testing.T) {
	got, err := NormalizeDeductAmount(0)
	if err != nil {
		t.Fatalf("0 金额应被允许: %v", err)
	}
	if got != 0 {
		t.Errorf("收敛结果 = %v, want 0", got)
	}
}

func TestNormalizeDeductAmountRejectsInvalidNumbers(t *testing.T) {
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := NormalizeDeductAmount(v); err == nil {
			t.Errorf("%v 应被拒绝：NaN/Inf 在 Postgres 上是合法 numeric，落库后无法对账", v)
		}
	}
}

// ── 账本边界：NormalizeRefundAmount ──

// 退款允许收敛成 0（空操作）：它来自库里已存的 task.Credits，
// 退款报错会连累任务状态转换失败，让任务卡在非终态反复重试。
func TestNormalizeRefundAmountAllowsRoundingToZero(t *testing.T) {
	got, err := NormalizeRefundAmount(0.0004)
	if err != nil {
		t.Fatalf("退款低于最小单位不该报错: %v", err)
	}
	if got != 0 {
		t.Errorf("收敛结果 = %v, want 0", got)
	}
}

// 负数退款等于反向扣费，绕过了扣费侧的记账
func TestNormalizeRefundAmountRejectsNegative(t *testing.T) {
	if _, err := NormalizeRefundAmount(-1); err == nil {
		t.Error("负数退款应被拒绝")
	}
}

func TestNormalizeRefundAmountRejectsInvalidNumbers(t *testing.T) {
	for _, v := range []float64{math.NaN(), math.Inf(1)} {
		if _, err := NormalizeRefundAmount(v); err == nil {
			t.Errorf("%v 应被拒绝", v)
		}
	}
}

// 扣多少、退多少必须是同一个数：两条路径对同一输入收敛结果一致
func TestDeductAndRefundAgreeOnSameAmount(t *testing.T) {
	for _, v := range []float64{0.001, 0.003, 1.5, 12.345, 0.1 + 0.2} {
		deducted, err := NormalizeDeductAmount(v)
		if err != nil {
			t.Fatalf("NormalizeDeductAmount(%v) 报错: %v", v, err)
		}
		refunded, err := NormalizeRefundAmount(v)
		if err != nil {
			t.Fatalf("NormalizeRefundAmount(%v) 报错: %v", v, err)
		}
		if deducted != refunded {
			t.Errorf("%v: 扣费收敛为 %v，退款收敛为 %v，二者必须一致", v, deducted, refunded)
		}
	}
}
