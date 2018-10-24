//
// decompress.go
//
// Copyright (c) 2018 Markku Rossi
//
// All rights reserved.
//

package xpress

import (
	"errors"
	"fmt"
)

var TruncatedInput = errors.New("Truncated input")

type SymbolLength []byte

func (sl SymbolLength) Length(sym int) int {
	if (sym % 2) == 0 {
		return int(sl[sym/2] & 0x0f)
	} else {
		return int(sl[sym/2] >> 4)
	}
}

type input struct {
	input []byte
	pos   int
}

func (in *input) ReadUint32() (uint32, error) {
	if in.pos+4 > len(in.input) {
		return 0, TruncatedInput
	}
	var val uint32
	for i := 0; i < 4; i++ {
		val |= (uint32(in.input[in.pos]) << uint(i*8))
		in.pos++
	}
	return val, nil
}

func (in *input) ReadUint16() (uint16, error) {
	if in.pos+2 > len(in.input) {
		return 0, TruncatedInput
	}
	var val uint16
	for i := 0; i < 2; i++ {
		val |= (uint16(in.input[in.pos]) << uint(i*8))
		in.pos++
	}
	return val, nil
}

func (in *input) ReadByte() (byte, error) {
	if in.pos >= len(in.input) {
		return 0, TruncatedInput
	}
	in.pos++
	return in.input[in.pos-1], nil
}

func (in *input) Avail() int {
	return len(in.input) - in.pos
}

const huffmanTableLength = 32768

func DecompressLZ77Huffman(data []byte, out []byte) ([]byte, error) {
	if len(data) < 256 {
		return out, errors.New("Invalid data")
	}

	var symLen SymbolLength = data[0:256]
	var currentTableEntry int
	var decodingTable [huffmanTableLength]uint16

	for bitLength := 1; bitLength <= 15; bitLength++ {
		for symbol := 0; symbol < 512; symbol++ {
			if symLen.Length(symbol) == bitLength {
				entryCount := (1 << uint(15-bitLength))
				for e := 0; e < entryCount; e++ {
					if currentTableEntry >= huffmanTableLength {
						return out, fmt.Errorf("Invalid Huffman table")
					}
					decodingTable[currentTableEntry] = uint16(symbol)
					currentTableEntry++
				}
			}
		}
	}
	if currentTableEntry != huffmanTableLength {
		return out, errors.New("Huffman table underflow")
	}

	// Inflate data.
	in := &input{
		input: data,
		pos:   256,
	}
	b, err := in.ReadUint16()
	if err != nil {
		return out, err
	}
	nextBits := uint32(b) << 16
	b, err = in.ReadUint16()
	if err != nil {
		return out, err
	}
	nextBits |= uint32(b)
	extraBits := 16

	// Loop until a terminating condition.
	for {
		next15Bits := nextBits >> (32 - 15)
		huffmanSymbol := decodingTable[next15Bits]
		huffmanSymbolBitLength := symLen.Length(int(huffmanSymbol))

		nextBits <<= uint(huffmanSymbolBitLength)
		extraBits -= huffmanSymbolBitLength

		if extraBits < 0 {
			b, err := in.ReadUint16()
			if err != nil {
				return out, err
			}
			nextBits |= uint32(b) << uint(-extraBits)
			extraBits += 16
		}
		if huffmanSymbol < 256 {
			out = append(out, byte(huffmanSymbol))
		} else if huffmanSymbol == 256 && in.Avail() == 0 {
			return out, nil
		} else {
			huffmanSymbol = huffmanSymbol - 256
			matchLength := huffmanSymbol % 16
			matchOffsetBitLength := huffmanSymbol / 16
			if matchLength == 15 {
				b, err := in.ReadByte()
				if err != nil {
					return out, err
				}
				matchLength = uint16(b)
				if matchLength == 255 {
					b, err := in.ReadUint16()
					if err != nil {
						return out, err
					}
					matchLength = b
					if matchLength < 15 {
						return out, errors.New("Invalid data")
					}
					matchLength -= 15
				}
				matchLength += 15
			}
			matchLength += 3
			matchOffset := nextBits >> (32 - matchOffsetBitLength)
			matchOffset += (1 << matchOffsetBitLength)
			nextBits <<= matchOffsetBitLength
			extraBits -= int(matchOffsetBitLength)
			if extraBits < 0 {
				b, err := in.ReadUint16()
				if err != nil {
					return out, err
				}
				nextBits |= uint32(b) << uint(-extraBits)
				extraBits += 16
			}
			for i := 0; i < int(matchLength); i++ {
				b := out[len(out)-int(matchOffset)]
				out = append(out, b)
			}
		}
	}
}

func DecompressLZ77(data []byte) ([]byte, error) {
	out := make([]byte, 0, len(data)*3)
	in := &input{
		input: data,
	}
	var err error

	var bufferedFlags uint32
	var bufferedFlagCount uint
	var lastLengthHalfByte int

	// Loop until break instruction or error
	for {
		if bufferedFlagCount == 0 {
			bufferedFlags, err = in.ReadUint32()
			if err != nil {
				return nil, err
			}
			bufferedFlagCount = 32
		}
		bufferedFlagCount--
		if (bufferedFlags & (1 << bufferedFlagCount)) == 0 {
			// Copy 1 byte from input to output
			b, err := in.ReadByte()
			if err != nil {
				return nil, err
			}
			out = append(out, b)
		} else {
			if in.Avail() == 0 {
				return out, nil
			}
			matchBytes, err := in.ReadUint16()
			if err != nil {
				return nil, err
			}
			matchLength := matchBytes % 8
			matchOffset := (matchBytes / 8) + 1

			if matchLength == 7 {
				if lastLengthHalfByte == 0 {
					b, err := in.ReadByte()
					if err != nil {
						return nil, err
					}
					matchLength = uint16(b % 16)
					lastLengthHalfByte = in.pos - 1
				} else {
					b := in.input[lastLengthHalfByte]
					matchLength = uint16(b / 16)
					lastLengthHalfByte = 0
				}
				if matchLength == 15 {
					b, err := in.ReadByte()
					if err != nil {
						return nil, err
					}
					matchLength = uint16(b)
					if matchLength == 255 {
						matchLength, err = in.ReadUint16()
						if err != nil {
							return nil, err
						}
						if matchLength < 15+7 {
							return nil, errors.New("!=15+7")
						}
						matchLength -= (15 + 7)
					}
					matchLength += 15
				}
				matchLength += 7
			}
			matchLength += 3
			for i := 0; i < int(matchLength); i++ {
				if int(matchOffset) > len(out) {
					fmt.Printf("outputPosition=%d, matchOffset=%d\n",
						len(out), matchOffset)
					continue
				}
				b := out[len(out)-int(matchOffset)]
				out = append(out, b)
			}
		}
	}
}

func DecompressLZNT1(data []byte) ([]byte, error) {
	out := make([]byte, 0, len(data))
	in := &input{
		input: data,
	}

	for in.Avail() > 0 {
		hdr, err := in.ReadUint16()
		if err != nil {
			return nil, err
		}
		format := (hdr >> 12) & 0x7
		len := int(hdr & 0xfff)

		var compressed bool

		if (hdr & 0x8000) != 0 {
			compressed = true
			if format != 3 {
				return nil, fmt.Errorf("Invalid compression format %d", format)
			}
		} else {
			len += 3
		}

		if compressed {
			return nil, errors.New("Compressed LZNT1")
		} else {
			if in.Avail() < len {
				return nil, TruncatedInput
			}
			out = append(out, in.input[in.pos:in.pos+len]...)
			in.pos += len
		}
	}
	return out, nil
}
