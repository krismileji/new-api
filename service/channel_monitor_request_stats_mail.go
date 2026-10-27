package service

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

const requestStatsMailSubject = "【API Key 使用提醒】您的一枚 API Key 已暂停"

func requestStatsMailContent(record model.ChannelMonitorRequestStatsDisable, displayName string) (string, string) {
	if strings.TrimSpace(displayName) == "" {
		displayName = "您好"
	}
	keyName := record.APIKeyName
	if strings.TrimSpace(keyName) == "" {
		keyName = fmt.Sprintf("API Key #%d", record.APIKeyId)
	}
	rate := float64(0)
	if record.KnownInputRequests > 0 {
		rate = float64(record.LowInputCount) / float64(record.KnownInputRequests) * 100
	}
	plain := fmt.Sprintf("%s：\n\n您的一枚 API Key 已暂停使用。\nAPI Key：%s（ID：%d）\n统计日期：%s（北京时间）\n输入阈值：%d Token（严格小于）\n低输入请求：%d / %d 个有效输入样本\n低输入请求占比：%.2f%%\n自动暂停条件：占比达到 %d%%，且有效输入样本不少于 %d 次\n\n这是普通禁用状态。您可以登录现有 API Key 管理页面自行启用；过期、额度不足及其他独立限制仍然有效。\n本通知仅提示请求特征，不代表已认定蒸馏或违规。未知输入 Token 的请求未参与占比计算。\n如非您本人使用，请先检查该 Key 的使用情况，必要时更换 Key。\n",
		displayName, keyName, record.APIKeyId, time.Unix(record.DayStart, 0).In(requestStatsLocation).Format(time.DateOnly), record.ThresholdK*1000,
		record.LowInputCount, record.KnownInputRequests, rate, record.DisableRatePercent, record.MinimumKnownInputRequests)
	markup := "<!doctype html><html lang=\"zh-CN\"><meta charset=\"utf-8\"><body><div style=\"font-family:Arial,sans-serif;line-height:1.7;max-width:640px;margin:24px auto;white-space:pre-wrap\">" + html.EscapeString(plain) + "</div></body></html>"
	return plain, markup
}

func deliverRequestStatsNotifications(ctx context.Context) error {
	now := time.Now().Unix()
	var records []model.ChannelMonitorRequestStatsDisable
	if err := model.DB.WithContext(ctx).Where("mail_status IN ? AND mail_next_at <= ? AND mail_lease_until <= ?", []string{"pending", "retry", "sending"}, now, now).
		Order("id ASC").Limit(10).Find(&records).Error; err != nil {
		return err
	}
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		owner := common.GetUUID()
		claim := model.DB.WithContext(ctx).Model(&model.ChannelMonitorRequestStatsDisable{}).
			Where("id = ? AND mail_status IN ? AND mail_next_at <= ? AND mail_lease_until <= ?", record.Id, []string{"pending", "retry", "sending"}, now, now).
			Updates(map[string]any{"mail_status": "sending", "mail_lease_owner": owner, "mail_lease_until": now + 60, "mail_attempts": gorm.Expr("mail_attempts + 1")})
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected != 1 {
			continue
		}
		status, message, next := "sent", "", int64(0)
		var user struct {
			Id          int
			Email       string
			DisplayName string
		}
		var receipt model.VerifiedNotificationEmail
		userErr := model.DB.WithContext(ctx).Model(&model.User{}).Select("id", "email", "display_name").Where("id = ?", record.UserId).First(&user).Error
		receiptErr := model.DB.WithContext(ctx).Where("user_id = ?", record.UserId).First(&receipt).Error
		if userErr != nil && !errors.Is(userErr, gorm.ErrRecordNotFound) || receiptErr != nil && !errors.Is(receiptErr, gorm.ErrRecordNotFound) {
			status, message, next = "retry", "查询通知邮箱失败，稍后重试", now+300
		} else if userErr != nil || receiptErr != nil || receipt.VerifiedAt <= 0 || model.NormalizeEmail(user.Email) == "" || model.NormalizeEmail(user.Email) != receipt.Email {
			status, message = "skipped", "没有当前邮箱的验证记录，已跳过通知"
		} else {
			plain, markup := requestStatsMailContent(record, user.DisplayName)
			// Deterministic per committed event; SMTP is at-least-once on an
			// ambiguous connection loss after DATA acceptance.
			messageID := fmt.Sprintf("<request-stats-%d-%d@new-api.local>", record.DayStart, record.APIKeyId)
			if err := common.SendEmailAlternativeWithTimeout(requestStatsMailSubject, user.Email, plain, markup, messageID, 20*time.Second); err != nil {
				status, message, next = "retry", "邮件发送失败，稍后重试", now+int64(min(3600, 60*(1<<min(record.MailAttempts+1, 6))))
			}
		}
		if status == "retry" && record.MailAttempts+1 >= 5 {
			status = "failed"
			message = "邮件通知失败，已达到 5 次尝试上限"
		}
		if err := model.DB.WithContext(ctx).Model(&model.ChannelMonitorRequestStatsDisable{}).Where("id = ? AND mail_lease_owner = ?", record.Id, owner).
			Updates(map[string]any{"mail_status": status, "mail_last_error": message, "mail_next_at": next, "mail_lease_owner": "", "mail_lease_until": 0}).Error; err != nil {
			return err
		}
	}
	return nil
}
