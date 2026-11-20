package cams

// bitReader — RBSP bit oqimi (Exp-Golomb bilan). emulation_prevention_three_byte
// (00 00 03) oldindan o'chirilgan bo'lishi kerak (stripEmulationPrevention).
type bitReader struct {
	data []byte
	bit  int
	err  error
}

func newBitReader(b []byte) *bitReader { return &bitReader{data: b} }

// u — n bit (n ≤ 32) o'qiydi.
func (b *bitReader) u(n int) uint32 {
	if b.err != nil {
		return 0
	}
	if n <= 0 || n > 32 {
		b.err = ErrBadSPS
		return 0
	}
	if b.bit+n > len(b.data)*8 {
		b.err = ErrBadSPS
		return 0
	}
	var v uint32
	for i := 0; i < n; i++ {
		v = v<<1 | b.bitAt(b.bit+i)
	}
	b.bit += n
	return v
}

func (b *bitReader) u1() uint32 { return b.u(1) }

// ue — Exp-Golomb (unsigned).
func (b *bitReader) ue() uint32 {
	if b.err != nil {
		return 0
	}
	zeros := 0
	for zeros < 32 {
		bit := b.u(1)
		if b.err != nil {
			return 0
		}
		if bit == 1 {
			break
		}
		zeros++
	}
	if zeros == 0 {
		return 0
	}
	return (1 << zeros) - 1 + b.u(zeros)
}

// se — Exp-Golomb (signed).
func (b *bitReader) se() int32 {
	k := b.ue()
	if b.err != nil {
		return 0
	}
	v := int32((k + 1) / 2)
	if k%2 == 0 {
		v = -v
	}
	return v
}

func (b *bitReader) bitAt(i int) uint32 {
	return uint32(b.data[i>>3]>>(7-uint(i&7))) & 1
}

// stripEmulationPrevention — 00 00 03 ketma-ketligidagi 03 ni olib tashlaydi
// (SPS/PPS matnida NAL raqamlanishini taqlid qiluvchi baytlar).
func stripEmulationPrevention(in []byte) []byte {
	out := make([]byte, 0, len(in))
	zeros := 0
	for _, v := range in {
		if zeros >= 2 && v == 0x03 {
			zeros = 0
			continue
		}
		if v == 0x00 {
			zeros++
		} else {
			zeros = 0
		}
		out = append(out, v)
	}
	return out
}
