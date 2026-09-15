package vlm

import (
	"encoding/binary"
	"strings"
	"unicode/utf16"
)

const (
	emfSignature         = 0x464D4520 // " EMF"
	emfRecordExtTextOutA = 0x00000053
	emfRecordExtTextOutW = 0x00000054
	emfETOGlyphIndex     = 0x0010

	wmfPlaceableKey   = 0x9AC6CDD7
	wmfMetaExtTextOut = 0x0A32
	wmfMetaTextOut    = 0x0521
	wmfETOOpaque      = 0x0002
	wmfETOClipped     = 0x0004
)

// IsMetafileImage reports whether data is WMF/EMF by binary magic.
// Path/MIME hints alone are not trusted — Office pipelines sometimes label PNG
// bytes as image/x-emf.
func IsMetafileImage(data []byte, sourceHint string) bool {
	_ = sourceHint
	return isEMF(data) || isWMF(data)
}

// ExtractMetafileText pulls embedded text records from WMF/EMF without rendering
// or calling a VLM. Returns empty string when no usable Unicode/ANSI text is found
// (e.g. glyph-index-only drawings).
func ExtractMetafileText(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	var parts []string
	switch {
	case isEMF(data):
		parts = extractEMFText(data)
	case isWMF(data):
		parts = extractWMFText(data)
	default:
		return ""
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func isEMF(data []byte) bool {
	if len(data) < 44 {
		return false
	}
	// ENHMETAHEADER.dSignature at offset 40.
	if data[40] == ' ' && data[41] == 'E' && data[42] == 'M' && data[43] == 'F' {
		return true
	}
	return binary.LittleEndian.Uint32(data[40:44]) == emfSignature
}

func isWMF(data []byte) bool {
	if len(data) >= 4 && binary.LittleEndian.Uint32(data[0:4]) == wmfPlaceableKey {
		return true
	}
	// Standard metafile header: Type=1 or 2, HeaderSize=9 (in WORDs).
	if len(data) >= 6 {
		ft := binary.LittleEndian.Uint16(data[0:2])
		hs := binary.LittleEndian.Uint16(data[2:4])
		if (ft == 1 || ft == 2) && hs == 9 {
			return true
		}
	}
	return false
}

func extractEMFText(data []byte) []string {
	var out []string
	for off := 0; off+8 <= len(data); {
		iType := binary.LittleEndian.Uint32(data[off : off+4])
		nSize := binary.LittleEndian.Uint32(data[off+4 : off+8])
		if nSize < 8 || int(nSize) > len(data)-off {
			break
		}
		rec := data[off : off+int(nSize)]
		switch iType {
		case emfRecordExtTextOutW:
			if s := parseEMRExtTextOut(rec, true); s != "" {
				out = append(out, s)
			}
		case emfRecordExtTextOutA:
			if s := parseEMRExtTextOut(rec, false); s != "" {
				out = append(out, s)
			}
		}
		off += int(nSize)
	}
	return out
}

// parseEMRExtTextOut extracts text from EMR_EXTTEXTOUTA/W.
// Layout: header(8) + Bounds(16) + iGraphicsMode(4) + exScale(4) + eyScale(4) + EmrText.
func parseEMRExtTextOut(rec []byte, wide bool) string {
	const emrTextOff = 36
	if len(rec) < emrTextOff+20 {
		return ""
	}
	nChars := int(binary.LittleEndian.Uint32(rec[emrTextOff+8 : emrTextOff+12]))
	offString := int(binary.LittleEndian.Uint32(rec[emrTextOff+12 : emrTextOff+16]))
	options := binary.LittleEndian.Uint32(rec[emrTextOff+16 : emrTextOff+20])
	if nChars <= 0 || nChars > 1<<20 {
		return ""
	}
	if options&emfETOGlyphIndex != 0 {
		// Glyph indices are font-specific; cannot recover Unicode reliably.
		return ""
	}
	if offString < 0 || offString >= len(rec) {
		return ""
	}
	if wide {
		need := offString + nChars*2
		if need > len(rec) {
			return ""
		}
		u16 := make([]uint16, nChars)
		for i := 0; i < nChars; i++ {
			u16[i] = binary.LittleEndian.Uint16(rec[offString+i*2 : offString+i*2+2])
		}
		return strings.TrimSpace(string(utf16.Decode(u16)))
	}
	need := offString + nChars
	if need > len(rec) {
		return ""
	}
	// ANSI / code-page bytes; Latin-1 is a reasonable fallback for embedded Office art.
	b := rec[offString : offString+nChars]
	runes := make([]rune, len(b))
	for i, c := range b {
		runes[i] = rune(c)
	}
	return strings.TrimSpace(string(runes))
}

func extractWMFText(data []byte) []string {
	off := 0
	if len(data) >= 22 && binary.LittleEndian.Uint32(data[0:4]) == wmfPlaceableKey {
		off = 22 // skip PLACEABLEHEADER
	}
	// Standard METAHEADER is 9 WORDs = 18 bytes.
	if off+18 > len(data) {
		return nil
	}
	off += 18

	var out []string
	for off+6 <= len(data) {
		sizeWords := binary.LittleEndian.Uint32(data[off : off+4])
		if sizeWords < 3 {
			break
		}
		byteSize := int(sizeWords) * 2
		if byteSize < 6 || off+byteSize > len(data) {
			break
		}
		fn := binary.LittleEndian.Uint16(data[off+4 : off+6])
		rec := data[off : off+byteSize]
		switch fn {
		case wmfMetaExtTextOut:
			if s := parseWMFExtTextOut(rec); s != "" {
				out = append(out, s)
			}
		case wmfMetaTextOut:
			if s := parseWMFTextOut(rec); s != "" {
				out = append(out, s)
			}
		}
		off += byteSize
	}
	return out
}

// META_EXTTEXTOUT: size(4) func(2) Y(2) X(2) StringLength(2) fwOpts(2) [Rect 8] String [pad] [Dx]
func parseWMFExtTextOut(rec []byte) string {
	if len(rec) < 14 {
		return ""
	}
	strLen := int(int16(binary.LittleEndian.Uint16(rec[10:12])))
	fwOpts := binary.LittleEndian.Uint16(rec[12:14])
	if strLen <= 0 || strLen > 1<<20 {
		return ""
	}
	pos := 14
	if fwOpts&(wmfETOOpaque|wmfETOClipped) != 0 {
		pos += 8
	}
	if pos+strLen > len(rec) {
		return ""
	}
	return strings.TrimSpace(string(rec[pos : pos+strLen]))
}

// META_TEXTOUT: size(4) func(2) StringLength(2) String [pad] Y(2) X(2)
func parseWMFTextOut(rec []byte) string {
	if len(rec) < 8 {
		return ""
	}
	strLen := int(int16(binary.LittleEndian.Uint16(rec[6:8])))
	if strLen <= 0 || strLen > 1<<20 {
		return ""
	}
	if 8+strLen > len(rec) {
		return ""
	}
	return strings.TrimSpace(string(rec[8 : 8+strLen]))
}
