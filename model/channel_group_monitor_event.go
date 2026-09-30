package model

// ChannelGroupMonitorExecutionEvent is installed once by the service package.
// It freezes the current Redis generation without IO inside the DB transaction.
// Returning nil means monitoring has not initialized or this execution belongs
// to an obsolete configuration. The execution remains available for admin audit.
var ChannelGroupMonitorExecutionEvent func(ChannelGroupMonitorExecution) *ChannelMonitorEvent
