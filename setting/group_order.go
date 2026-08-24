package setting

import (
	"fmt"
	"sort"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

var (
	groupOrderMu sync.RWMutex
	groupOrder   []string
)

// GetGroupOrder returns the configured pricing-group order. The returned
// slice is a copy and can be safely modified by the caller.
func GetGroupOrder() []string {
	groupOrderMu.RLock()
	defer groupOrderMu.RUnlock()
	return append([]string(nil), groupOrder...)
}

// GroupOrder2JsonString returns the configured pricing-group order as JSON.
func GroupOrder2JsonString() string {
	data, err := common.Marshal(GetGroupOrder())
	if err != nil {
		return "[]"
	}
	return string(data)
}

// UpdateGroupOrderByJsonString replaces the configured pricing-group order.
func UpdateGroupOrderByJsonString(value string) error {
	var next []string
	if err := common.Unmarshal([]byte(value), &next); err != nil {
		return err
	}
	if next == nil {
		next = []string{}
	}
	if err := ValidateGroupOrder(next); err != nil {
		return err
	}

	groupOrderMu.Lock()
	groupOrder = append([]string(nil), next...)
	groupOrderMu.Unlock()
	return nil
}

func ValidateGroupOrder(value []string) error {
	seen := make(map[string]struct{}, len(value))
	for _, group := range value {
		if group == "" {
			return fmt.Errorf("group order contains an empty group name")
		}
		if _, ok := seen[group]; ok {
			return fmt.Errorf("group order contains duplicate group: %s", group)
		}
		seen[group] = struct{}{}
	}
	return nil
}

// SortGroupNames applies the configured order and keeps unconfigured groups
// available as a deterministic, name-sorted tail. This lets older
// installations adopt the setting without losing newly added groups.
func SortGroupNames(groups []string) []string {
	ordered := append([]string(nil), groups...)
	ranks := make(map[string]int)
	for index, group := range GetGroupOrder() {
		ranks[group] = index
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		leftRank, leftConfigured := ranks[ordered[i]]
		rightRank, rightConfigured := ranks[ordered[j]]
		if leftConfigured && rightConfigured {
			return leftRank < rightRank
		}
		if leftConfigured != rightConfigured {
			return leftConfigured
		}
		return ordered[i] < ordered[j]
	})
	return ordered
}
