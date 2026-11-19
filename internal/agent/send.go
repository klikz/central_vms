package agent

import "central-vms/client/internal/proto"

// Send — serverga xabar yuborish (yagona yozuvchi goroutine navbatiga qo'yadi).
// Sessiya faol bo'lmasa ErrChannelClosed qaytadi.
func (s *Session) Send(typ string, data any) error {
	env, err := proto.NewEnvelope(typ, s.nextSeq(), data)
	if err != nil {
		return err
	}
	return s.trySend(env)
}
