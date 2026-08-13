package main

// IMA ADPCM decoder as used by KiwiSDR (libcsdr ima_adpcm_e8).
// 4-bit nibbles, low nibble first. State carries across frames.

var indexAdjustTable = [16]int{-1, -1, -1, -1, 2, 4, 6, 8, -1, -1, -1, -1, 2, 4, 6, 8}

var stepSizeTable = [...]int{
	7, 8, 9, 10, 11, 12, 13, 14, 16, 17, 19, 21, 23, 25, 28, 31, 34,
	37, 41, 45, 50, 55, 60, 66, 73, 80, 88, 97, 107, 118, 130, 143,
	157, 173, 190, 209, 230, 253, 279, 307, 337, 371, 408, 449, 494,
	544, 598, 658, 724, 796, 876, 963, 1060, 1166, 1282, 1411, 1552,
	1707, 1878, 2066, 2272, 2499, 2749, 3024, 3327, 3660, 4026,
	4428, 4871, 5358, 5894, 6484, 7132, 7845, 8630, 9493, 10442,
	11487, 12635, 13899, 15289, 16818, 18500, 20350, 22385, 24623,
	27086, 29794, 32767,
}

type adpcmState struct {
	index int
	prev  int
}

func (s *adpcmState) reset() {
	s.index = 0
	s.prev = 0
}

func (s *adpcmState) decodeNibble(nibble byte) int16 {
	step := stepSizeTable[s.index]
	diff := step >> 3
	if nibble&1 != 0 {
		diff += step >> 2
	}
	if nibble&2 != 0 {
		diff += step >> 1
	}
	if nibble&4 != 0 {
		diff += step
	}
	if nibble&8 != 0 {
		diff = -diff
	}
	v := s.prev + diff
	if v > 32767 {
		v = 32767
	} else if v < -32768 {
		v = -32768
	}
	s.prev = v
	s.index += indexAdjustTable[nibble]
	if s.index < 0 {
		s.index = 0
	} else if s.index > 88 {
		s.index = 88
	}
	return int16(v)
}

// decodeByte decodes one byte into two samples (low nibble first).
func (s *adpcmState) decodeByte(b byte, out []int16) {
	out[0] = s.decodeNibble(b & 0x0f)
	out[1] = s.decodeNibble((b >> 4) & 0x0f)
}
