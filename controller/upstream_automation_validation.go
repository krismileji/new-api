package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

func upstreamAutomationConfigDecodeMessage(err error) string {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return "自动任务配置请求超过 128 KiB，请减少请求正文或变量内容"
	}
	var fieldError *json.UnmarshalTypeError
	if errors.As(err, &fieldError) {
		field := strings.TrimPrefix(fieldError.Field, "UpstreamAutomationConfig.")
		switch field {
		case "interval_minutes":
			return "检查间隔（分钟）必须以整数提交，请刷新页面后重试"
		case "request_timeout":
			return "请求超时（秒）必须以整数提交，请刷新页面后重试"
		case "":
			return "自动任务配置必须是 JSON 对象"
		default:
			return fmt.Sprintf("自动任务配置字段「%s」类型无效，请检查填写内容", field)
		}
	}
	return "自动任务配置 JSON 格式无效或内容不完整，请刷新页面后重试"
}
