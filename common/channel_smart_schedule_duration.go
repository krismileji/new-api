package common

// ChannelMonitorSmartSchedulePermanentUntil keeps manual scheduling states active
// until cleared, using existing timestamp comparisons without a schema change.
// The sentinel is an exact, safe integer in Go, JavaScript and Redis sorted sets.
const ChannelMonitorSmartSchedulePermanentUntil int64 = 1 << 52
