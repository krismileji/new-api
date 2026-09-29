package relay

// stateMu serializes draining with admission and the terminal response write.
// The worker finishes billing and sends its terminal frame before closing.
func (s *responsesWSSession) drainChannel(reason string) {
	s.stateMu.Lock()
	if s.channelDrainReason == "" {
		s.channelDrainReason = reason
	}
	idle := s.current == nil
	s.stateMu.Unlock()
	if idle {
		s.closeForPolicy(reason)
	}
}
