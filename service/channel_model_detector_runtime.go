package service

import "sync"

var channelModelDetectorRuntime = struct {
	sync.Once
	store *ChannelModelDetectorTokenStore
	err   error
}{}

// GetChannelModelDetectorTokenStore returns the process-wide credential store
// shared by the detector worker issuer and the internal fixed-channel relay.
func GetChannelModelDetectorTokenStore() (*ChannelModelDetectorTokenStore, error) {
	channelModelDetectorRuntime.Do(func() {
		channelModelDetectorRuntime.store, channelModelDetectorRuntime.err = NewChannelModelDetectorTokenStore()
	})
	return channelModelDetectorRuntime.store, channelModelDetectorRuntime.err
}
