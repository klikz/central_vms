package agent

import "central-vms/client/internal/proto"

// Send — serverga xabar yuborish (yagona yozuvchi goroutine navbatiga qo'yadi).
// Sessiya faol bo'lmasa ErrChannelClosed qaytadi.
func (s *Session) Send(typ string, data any) error {
	ch := s.getSendCh()
	if ch == nil {
		return ErrChannelClosed
	}
	env, err := proto.NewEnvelope(typ, s.nextSeq(), data)
	if err != nil {
		return err
	}
	select {
	case ch <- env:
		return nil
	default:
		return ErrChannelClosed
	}
}
