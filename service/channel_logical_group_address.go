package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/model"
)

const maxLogicalChannelAddressLength = 2048

var (
	ErrLogicalChannelAddressEmpty        = errors.New("渠道请求地址不能为空")
	ErrLogicalChannelAddressInvalid      = errors.New("渠道请求地址无效")
	ErrLogicalChannelAddressScheme       = errors.New("渠道请求地址仅支持 HTTP 或 HTTPS")
	ErrLogicalChannelAddressCredentials  = errors.New("渠道请求地址不能包含账号密码")
	ErrLogicalChannelAddressEmptyMembers = errors.New("至少需要选择一个渠道成员")
)

// LogicalChannelAddressInput contains the effective upstream base URL.
type LogicalChannelAddressInput struct {
	ChannelID int
	Address   string
}

// LogicalChannelAddressPrecheckMember is safe for admin precheck responses.
type LogicalChannelAddressPrecheckMember struct {
	ChannelID          int    `json:"channel_id"`
	NormalizedAddress  string `json:"normalized_address,omitempty"`
	AddressFingerprint string `json:"address_fingerprint,omitempty"`
	Error              string `json:"error,omitempty"`
}

// LogicalChannelAddressPrecheckResult describes address compatibility.
type LogicalChannelAddressPrecheckResult struct {
	Compatible         bool                                  `json:"compatible"`
	NormalizedAddress  string                                `json:"normalized_address,omitempty"`
	AddressFingerprint string                                `json:"address_fingerprint,omitempty"`
	Members            []LogicalChannelAddressPrecheckMember `json:"members"`
	Error              string                                `json:"error,omitempty"`
}

// NormalizeLogicalChannelAddress canonicalizes an effective upstream URL.
// Compatibility intentionally depends only on this URL.
func NormalizeLogicalChannelAddress(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrLogicalChannelAddressEmpty
	}
	if len(raw) > maxLogicalChannelAddressLength {
		return "", fmt.Errorf("%w: 地址长度不能超过 %d 个字符", ErrLogicalChannelAddressInvalid, maxLogicalChannelAddressLength)
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Opaque != "" {
		return "", fmt.Errorf("%w: 缺少有效的 scheme 或 host", ErrLogicalChannelAddressInvalid)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", ErrLogicalChannelAddressScheme
	}
	if parsed.User != nil {
		return "", ErrLogicalChannelAddressCredentials
	}
	hostname := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		parsed.Host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		parsed.Host = "[" + hostname + "]"
	} else {
		parsed.Host = hostname
	}
	if parsed.RawPath != "" {
		// Keep escaped path separators (for example %2F) because they can be
		// meaningful upstream. Only literal trailing slash bytes are removed.
		rawPath := strings.TrimRight(parsed.EscapedPath(), "/")
		decodedPath, err := url.PathUnescape(rawPath)
		if err != nil {
			return "", fmt.Errorf("%w: 路径无效", ErrLogicalChannelAddressInvalid)
		}
		parsed.Path = decodedPath
		parsed.RawPath = rawPath
	} else {
		parsed.Path = strings.TrimRight(parsed.Path, "/")
	}
	parsed.Fragment = ""
	parsed.RawFragment = ""
	query, err := normalizeLogicalChannelAddressQuery(parsed.RawQuery)
	if err != nil {
		return "", fmt.Errorf("%w: 查询参数无效", ErrLogicalChannelAddressInvalid)
	}
	parsed.RawQuery = query
	parsed.ForceQuery = false
	return parsed.String(), nil
}

// NormalizeLogicalChannelAddressForChannel resolves a channel's effective URL.
func NormalizeLogicalChannelAddressForChannel(channel *model.Channel) (string, error) {
	if channel == nil {
		return "", ErrLogicalChannelAddressEmpty
	}
	return NormalizeLogicalChannelAddress(channel.GetBaseURL())
}

// LogicalChannelAddressFingerprint returns a non-reversible SHA-256 digest.
func LogicalChannelAddressFingerprint(normalizedAddress string) string {
	digest := sha256.Sum256([]byte(normalizedAddress))
	return hex.EncodeToString(digest[:])
}

// PrecheckLogicalChannelAddresses validates a proposed member set.
func PrecheckLogicalChannelAddresses(inputs []LogicalChannelAddressInput) LogicalChannelAddressPrecheckResult {
	result := LogicalChannelAddressPrecheckResult{Members: make([]LogicalChannelAddressPrecheckMember, 0, len(inputs))}
	if len(inputs) == 0 {
		result.Error = ErrLogicalChannelAddressEmptyMembers.Error()
		return result
	}
	for _, input := range inputs {
		member := LogicalChannelAddressPrecheckMember{ChannelID: input.ChannelID}
		normalized, err := NormalizeLogicalChannelAddress(input.Address)
		if err != nil {
			member.Error = err.Error()
			result.Members = append(result.Members, member)
			continue
		}
		member.NormalizedAddress = normalized
		member.AddressFingerprint = LogicalChannelAddressFingerprint(normalized)
		result.Members = append(result.Members, member)
		if result.NormalizedAddress == "" {
			result.NormalizedAddress = normalized
			result.AddressFingerprint = member.AddressFingerprint
		}
	}
	for _, member := range result.Members {
		if member.Error != "" {
			result.Error = member.Error
			return result
		}
		if member.NormalizedAddress != result.NormalizedAddress {
			result.Error = "渠道请求地址不一致"
			return result
		}
	}
	result.Compatible = true
	return result
}

// PrecheckLogicalChannelMembers resolves effective URLs from channel models.
func PrecheckLogicalChannelMembers(channels []*model.Channel) LogicalChannelAddressPrecheckResult {
	inputs := make([]LogicalChannelAddressInput, 0, len(channels))
	for _, channel := range channels {
		if channel == nil {
			inputs = append(inputs, LogicalChannelAddressInput{})
			continue
		}
		inputs = append(inputs, LogicalChannelAddressInput{ChannelID: channel.Id, Address: channel.GetBaseURL()})
	}
	return PrecheckLogicalChannelAddresses(inputs)
}

func normalizeLogicalChannelAddressQuery(rawQuery string) (string, error) {
	if rawQuery == "" {
		return "", nil
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "", err
	}
	filtered := make(url.Values, len(values))
	for key, entries := range values {
		if logicalChannelSensitiveQueryKey(key) {
			continue
		}
		filtered[key] = append([]string(nil), entries...)
	}
	for key := range filtered {
		sort.Strings(filtered[key])
	}
	return filtered.Encode(), nil
}

func logicalChannelSensitiveQueryKey(key string) bool {
	normalized := strings.ToLower(strings.Map(func(r rune) rune {
		switch r {
		case '-', '_', '.', ' ':
			return -1
		default:
			return r
		}
	}, strings.TrimSpace(key)))
	if normalized == "" {
		return false
	}
	for _, marker := range []string{"key", "token", "secret", "password", "passwd", "credential", "authorization", "signature", "access", "bearer", "jwt"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}
