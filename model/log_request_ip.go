package model

import "github.com/QuantumNous/new-api/common"

const adminRequestIPKey = "request_ip"

func attachAdminRequestIP(other map[string]interface{}, requestIP string) map[string]interface{} {
	if other == nil {
		other = make(map[string]interface{})
	}
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	if !ok {
		adminInfo = make(map[string]interface{})
		other["admin_info"] = adminInfo
	}
	adminInfo[adminRequestIPKey] = requestIP
	return other
}

func exposeAdminRequestIPs(logs []*Log) {
	for _, log := range logs {
		if log.Ip != "" {
			continue
		}
		other, err := common.StrToMap(log.Other)
		if err != nil {
			continue
		}
		adminInfo, ok := other["admin_info"].(map[string]interface{})
		if !ok {
			continue
		}
		requestIP, ok := adminInfo[adminRequestIPKey].(string)
		if ok {
			log.Ip = requestIP
		}
	}
}
