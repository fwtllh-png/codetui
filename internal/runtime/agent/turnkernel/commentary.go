package turnkernel

import "github.com/fwtllh-png/QCode/internal/runtime/protocol"

func (message Commentary) ProtocolData(turnID string) protocol.CommentaryCompletedData {
	return protocol.CommentaryCompletedData{
		MessageID: turnID + "/commentary/" + message.SampleID,
		SampleID:  message.SampleID,
		Text:      message.Text,
		CallIDs:   append([]string(nil), message.CallIDs...),
	}
}

// CommentaryMessages is used once at recovery, not on the sampling hot path.
func (s *RuntimeKernel) CommentaryMessages() []protocol.CommentaryCompletedData {
	s.mu.Lock()
	defer s.mu.Unlock()
	messages := make([]protocol.CommentaryCompletedData, 0, len(s.state.Commentary))
	for _, message := range s.state.Commentary {
		messages = append(messages, message.ProtocolData(s.coordinator.TurnID()))
	}
	return messages
}

func (s *RuntimeKernel) SampleCommentary(sampleID string) *protocol.CommentaryCompletedData {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.state.Commentary) == 0 {
		return nil
	}
	message := s.state.Commentary[len(s.state.Commentary)-1]
	if message.SampleID != sampleID {
		return nil
	}
	data := message.ProtocolData(s.coordinator.TurnID())
	return &data
}
