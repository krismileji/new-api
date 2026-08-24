package main

import (
	"fmt"
	"os"

	"github.com/QuantumNous/new-api/common"
)

func main() {
	for _, argument := range os.Args[1:] {
		if argument == "-h" || argument == "--help" {
			fmt.Print(usageText)
			return
		}
	}
	config, err := parseConfig(os.Args[1:], os.Getenv, os.ReadFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "参数错误:", err)
		os.Exit(2)
	}

	report, err := runAcceptance(config)
	if err != nil {
		fmt.Fprintln(os.Stderr, "验收执行失败:", err)
		os.Exit(1)
	}

	encoded, err := common.Marshal(report)
	if err != nil {
		fmt.Fprintln(os.Stderr, "生成报告失败:", err)
		os.Exit(1)
	}
	encoded, err = common.IndentJson(encoded)
	if err != nil {
		fmt.Fprintln(os.Stderr, "格式化报告失败:", err)
		os.Exit(1)
	}
	encoded = append(encoded, '\n')
	if config.reportFile != "" {
		if err := os.WriteFile(config.reportFile, encoded, 0o600); err != nil {
			fmt.Fprintln(os.Stderr, "写入报告失败:", err)
			os.Exit(1)
		}
	}
	_, _ = os.Stdout.Write(encoded)

	if report.FailedChecks > 0 {
		os.Exit(1)
	}
}
