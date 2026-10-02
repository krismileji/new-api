// channel-profit-reconcile reviews pending income and orphaned reservations with database-admin
// access. It does not change schema or start gateway background services.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	inputPath := flag.String("input", "", "已核实的收入核对 JSON 文件；不提供时只列出 pending 和 reserved，后者可能仍在请求中")
	apply := flag.Bool("apply", false, "应用 input 中的一条核对并保存审计记录；不修改资金余额")
	limit := flag.Int("limit", 100, "只读列表条数（1—1000）")
	after := flag.Int64("after-id", 0, "只读列表起始 ID（不含），用于继续分页")
	flag.Parse()
	if *limit < 1 || *limit > 1000 || *after < 0 || *apply && *inputPath == "" {
		return errors.New("参数无效；写入必须同时提供 -input 和 -apply")
	}
	var input model.ChannelMonitorIncomeReconciliation
	if *inputPath != "" {
		file, err := os.Open(*inputPath)
		if err != nil {
			return err
		}
		defer file.Close()
		stat, err := file.Stat()
		if err != nil || stat.Size() > 16*1024 {
			return errors.New("核对文件不可读取或超过 16 KiB")
		}
		if err := common.DecodeJson(file, &input); err != nil {
			return errors.New("核对文件必须是有效 JSON")
		}
	}
	var dialector gorm.Dialector
	var dbType common.DatabaseType
	dsn := os.Getenv("SQL_DSN")
	switch {
	case strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://"):
		dialector, dbType = postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), common.DatabaseTypePostgreSQL
	case dsn != "":
		dialector, dbType = mysql.Open(dsn), common.DatabaseTypeMySQL
	default:
		path := os.Getenv("PROFIT_SQLITE_PATH")
		if path == "" {
			return errors.New("必须显式设置 SQL_DSN 或 PROFIT_SQLITE_PATH，避免连接默认业务库")
		}
		if _, err := os.Stat(path); err != nil {
			return errors.New("指定的 SQLite 数据库必须已存在")
		}
		dialector, dbType = sqlite.Open(path), common.DatabaseTypeSQLite
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return errors.New("数据库连接失败，请检查显式配置")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	model.DB = db
	common.SetDatabaseTypes(dbType, dbType)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if *apply {
		if err := model.ReconcileChannelMonitorIncome(ctx, input); err != nil {
			return err
		}
		fmt.Println("收入核对已保存，资金余额未改变；成本或缺口未解除时利润仍待确认")
		return nil
	}
	query := db.WithContext(ctx).Model(&model.ChannelMonitorIncome{}).
		Select("id, settlement_key, user_id, channel_id, day_start, quota, income_nano_cny, quota_per_unit, billing_source, status, cost_recorded, updated_at, funding_token_id, funding_subscription_id")
	if *inputPath != "" {
		query = query.Where("settlement_key = ?", input.SettlementKey)
	} else {
		query = query.Where("status IN ? AND id > ?", []string{"pending", "reserved"}, *after)
	}
	var records []struct {
		ID                    int64  `json:"id"`
		SettlementKey         string `json:"settlement_key"`
		UserID                int    `json:"user_id"`
		ChannelID             int    `json:"channel_id"`
		DayStart              int64  `json:"day_start"`
		Quota                 int64  `json:"quota"`
		IncomeNanoCNY         int64  `json:"income_nano_cny"`
		QuotaPerUnit          string `json:"quota_per_unit"`
		BillingSource         string `json:"billing_source"`
		Status                string `json:"status"`
		CostRecorded          int    `json:"cost_recorded"`
		UpdatedAt             int64  `json:"updated_at"`
		FundingTokenID        int    `json:"funding_token_id"`
		FundingSubscriptionID int    `json:"funding_subscription_id"`
	}
	if err := query.Order("id").Limit(*limit).Find(&records).Error; err != nil {
		return err
	}
	output, err := common.Marshal(map[string]any{"read_only": true, "records": records, "proposed_review": input})
	if err != nil {
		return err
	}
	fmt.Println(string(output))
	return nil
}
