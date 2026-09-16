package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorCustomResultExpression(t *testing.T) {
	body := []byte(`{"data":{"total":120,"used":" 35 ","rate":2,"zero":0,"remaining-credit":85,"items":[{"name":"main","value":8}]},"=credit":4}`)
	tests := []struct {
		name       string
		path       string
		multiplier float64
		want       float64
	}{
		{"plain path", "data.total", 0.5, 60},
		{"numeric string", "data.used", 2, 70},
		{"key with operator", "data.remaining-credit", 1, 85},
		{"escaped leading equals", `\=credit`, 1, 4},
		{"GJSON query", `data.items.#(name=="main").value`, 1, 8},
		{"addition", `=json("data.total") + json("data.used")`, 2, 310},
		{"subtraction then multiplier", `=json("data.total") - json("data.used")`, 0.01, 0.85},
		{"multiplication", `=json("data.used") * json("data.rate")`, 0.5, 35},
		{"division", `=json("data.used") / json("data.rate")`, 1, 17.5},
		{"precedence", `=json("data.total") - json("data.used") * json("data.rate")`, 1, 50},
		{"parentheses", `=(json("data.total") - json("data.used")) / json("data.rate")`, 2, 85},
		{"left associative division", `=json("data.total") / 2 / 3`, 1, 20},
		{"fractional constant division", `=1 / 2`, 1, 0.5},
		{"signed decimals and exponents", `=-json("data.rate") + +1.5e1 - .5`, 1, 12.5},
		{"array path", `=json("data.items.0.value") + json("data.rate")`, 1, 10},
		{"escaped query string", `=json("data.items.#(name==\"main\").value") / 2`, 1, 4},
		{"explicit zero", `=json("data.zero") * json("data.total")`, 1, 0},
		{"negative balance", `=json("data.used") - json("data.total")`, 1, -85},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value, err := extractChannelMonitorCustomValue(body, ChannelMonitorCustomResultConfig{
				ResponseType: ChannelMonitorCustomResponseJSON, ValuePath: tt.path, Multiplier: tt.multiplier,
			})
			require.NoError(t, err)
			assert.InDelta(t, tt.want, value, 1e-12)
		})
	}
	t.Run("text responses keep the existing behavior", func(t *testing.T) {
		value, err := extractChannelMonitorCustomValue([]byte(" 2.5 \n"), ChannelMonitorCustomResultConfig{
			ResponseType: ChannelMonitorCustomResponseText, ValuePath: `=json("ignored")`, Multiplier: 2,
		})
		require.NoError(t, err)
		assert.Equal(t, 5.0, value)
	})
}

func TestChannelMonitorCustomResultExpressionRejectsInvalidValues(t *testing.T) {
	body := []byte(`{"total":120,"used":35,"zero":0,"empty":null,"object":{},"array":[1],"boolean":true,"text":"invalid","nan":"NaN","inf":"+Inf","large":1e308}`)
	tests := []struct {
		name       string
		path       string
		multiplier float64
		wantError  string
	}{
		{"missing field", `=json("total") - json("missing")`, 1, "不存在"},
		{"null field", `=json("empty") + 1`, 1, "不存在"},
		{"object field", `=json("object") + 1`, 1, "不是数字"},
		{"array field", `=json("array") + 1`, 1, "不是数字"},
		{"boolean field", `=json("boolean") + 1`, 1, "不是数字"},
		{"nonnumeric string", `=json("text") + 1`, 1, "不是有效数字"},
		{"NaN string", `=json("nan") + 1`, 1, "不是有效数字"},
		{"infinite string", `=json("inf") + 1`, 1, "不是有效数字"},
		{"division by zero", `=json("total") / json("zero")`, 1, "除数不能为 0"},
		{"computed zero divisor", `=1 / (json("used") - 35)`, 1, "除数不能为 0"},
		{"hidden zero divisor", `=1 / (1 / json("zero"))`, 1, "除数不能为 0"},
		{"arithmetic overflow", `=json("large") * 2`, 1, "数值溢出"},
		{"hidden intermediate overflow", `=1 / (json("large") * 2)`, 1, "数值溢出"},
		{"multiplier overflow", `=json("large")`, 2, "乘以结果乘数后不是有效数字"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := extractChannelMonitorCustomValue(body, ChannelMonitorCustomResultConfig{
				ResponseType: ChannelMonitorCustomResponseJSON, ValuePath: tt.path, Multiplier: tt.multiplier,
			})
			require.ErrorContains(t, err, tt.wantError)
		})
	}
	t.Run("invalid JSON even for a constant expression", func(t *testing.T) {
		_, err := extractChannelMonitorCustomValue([]byte("not json"), ChannelMonitorCustomResultConfig{
			ResponseType: ChannelMonitorCustomResponseJSON, ValuePath: "=1 + 2", Multiplier: 1,
		})
		require.ErrorContains(t, err, "不是有效 JSON")
	})
}

func TestChannelMonitorCustomResultExpressionValidation(t *testing.T) {
	for _, path := range []string{
		"=", `=json("total") +`, `=(json("total")`,
		`=json("")`, `=json("  ")`, `=json(total)`, `=json()`, `=json("a", "b")`,
		`=other("a")`, `=json("a").value`, `=json("a") % 2`, `=2 ** 3`,
		`="1" + 2`, `=true`, `=[1, 2]`, `=1 > 0 ? 1 : 0`,
		`=sum([1, 2])`, `=map(1..10, # * 2)`, `=let a = 1; a + 2`,
		"=" + strings.Repeat("1+", 256) + "1",
	} {
		t.Run(path, func(t *testing.T) {
			_, err := normalizeChannelMonitorCustomResult(ChannelMonitorCustomResultConfig{
				ResponseType: ChannelMonitorCustomResponseJSON, ValuePath: path, Multiplier: 1,
			})
			require.Error(t, err, "unsupported expressions must be rejected before saving or sending requests")
		})
	}
	t.Run("unresolved paths are allowed until the response is available", func(t *testing.T) {
		result, err := normalizeChannelMonitorCustomResult(ChannelMonitorCustomResultConfig{
			ResponseType: ChannelMonitorCustomResponseJSON, ValuePath: `  =json("a") / (json("b") - json("c"))  `, Multiplier: 2,
		})
		require.NoError(t, err)
		assert.Equal(t, `=json("a") / (json("b") - json("c"))`, result.ValuePath)
	})
}

func TestFetchChannelMonitorCustomResultExpressions(t *testing.T) {
	useChannelMonitorCustomTestFetchSettings(t)
	for _, reuse := range []bool{false, true} {
		name := "independent requests"
		if reuse {
			name = "shared response"
		}
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = w.Write([]byte(`{"data":{"total":120,"used":35,"price":3,"base":2}}`))
			}))
			defer server.Close()
			config := ChannelMonitorCustomUpstreamConfig{
				Ratio: ChannelMonitorCustomMetricConfig{
					Source:  ChannelMonitorCustomSourceHTTP,
					Request: &ChannelMonitorCustomRequestConfig{Method: "GET", Path: "/account"},
					Result: &ChannelMonitorCustomResultConfig{
						ResponseType: "json", ValuePath: `=json("data.price") / json("data.base")`, Multiplier: 2,
					},
				},
				Balance: ChannelMonitorCustomMetricConfig{
					Source:  ChannelMonitorCustomSourceHTTP,
					Request: &ChannelMonitorCustomRequestConfig{Method: "GET", Path: "/balance"},
					Result: &ChannelMonitorCustomResultConfig{
						ResponseType: "json", ValuePath: `=json("data.total") - json("data.used")`, Multiplier: 0.01,
					},
				},
				BalanceReuseRatioRequest: reuse,
			}
			// Exercise the same configuration round trip used by save and reload.
			raw, err := MarshalChannelMonitorCustomUpstreamConfig(config)
			require.NoError(t, err)
			saved, err := ParseChannelMonitorCustomUpstreamConfig(raw)
			require.NoError(t, err)
			assert.Equal(t, config.Ratio.Result, saved.Ratio.Result)
			assert.Equal(t, config.Balance.Result, saved.Balance.Result)
			result, err := fetchChannelMonitorCustomUpstreamRatio(t.Context(), server.Client(), server.URL, saved, false, false)
			require.NoError(t, err)
			assert.Equal(t, 3.0, result.Ratio)
			require.NotNil(t, result.Balance.Amount)
			assert.InDelta(t, 0.85, *result.Balance.Amount, 1e-12)
			wantCalls := int32(2)
			if reuse {
				wantCalls = 1
			}
			assert.Equal(t, wantCalls, calls.Load())

			// A failed balance expression preserves the successful ratio and reports
			// no balance, so callers cannot treat it as a legitimate zero balance.
			saved.Balance.Result.ValuePath = `=json("missing") - json("data.used")`
			partial, err := fetchChannelMonitorCustomUpstreamRatio(t.Context(), server.Client(), server.URL, saved, false, false)
			require.ErrorContains(t, err, "不存在")
			assert.Equal(t, 3.0, partial.Ratio)
			assert.Nil(t, partial.Balance.Amount)
			assert.NotEmpty(t, partial.Balance.Error)
		})
	}
}

func TestFetchChannelMonitorCustomResultExpressionBounds(t *testing.T) {
	useChannelMonitorCustomTestFetchSettings(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"value":1}`))
	}))
	defer server.Close()
	for _, tt := range []struct {
		name      string
		ratio     string
		balance   string
		wantError string
	}{
		{"negative ratio", `=json("value") - 2`, `=0`, "倍率必须在"},
		{"oversized ratio", `=json("value") + 1000000`, `=0`, "倍率必须在"},
		{"oversized balance", `=1`, `=json("value") + 1e15`, "余额不是有效数字或绝对值过大"},
		{"undersized balance", `=1`, `=-json("value") - 1e15`, "余额不是有效数字或绝对值过大"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := fetchChannelMonitorCustomUpstreamRatio(t.Context(), server.Client(), server.URL, ChannelMonitorCustomUpstreamConfig{
				Ratio: ChannelMonitorCustomMetricConfig{
					Source: "http", Request: &ChannelMonitorCustomRequestConfig{Method: "GET", Path: "/account"},
					Result: &ChannelMonitorCustomResultConfig{ResponseType: "json", ValuePath: tt.ratio, Multiplier: 1},
				},
				Balance: ChannelMonitorCustomMetricConfig{
					Source: "http", Result: &ChannelMonitorCustomResultConfig{ResponseType: "json", ValuePath: tt.balance, Multiplier: 1},
				},
				BalanceReuseRatioRequest: true,
			}, false, false)
			require.ErrorContains(t, err, tt.wantError)
		})
	}
}
