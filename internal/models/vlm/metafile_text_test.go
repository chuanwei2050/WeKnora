package vlm

import (
	"encoding/binary"
	"testing"
	"unicode/utf16"
)

func TestExtractMetafileTextFromEMF(t *testing.T) {
	data := buildTestEMF("Hello 你好")
	if !IsMetafileImage(data, "img.png") {
		t.Fatal("expected EMF magic detection")
	}
	got := ExtractMetafileText(data)
	if got != "Hello 你好" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractMetafileTextSkipsGlyphIndex(t *testing.T) {
	data := buildTestEMFWithOptions("ABC", emfETOGlyphIndex)
	if got := ExtractMetafileText(data); got != "" {
		t.Fatalf("glyph-index text should be ignored, got %q", got)
	}
}

func TestPrepareImageForVLMSkipsMetafile(t *testing.T) {
	data := buildTestEMF("x")
	_, skip, err := PrepareImageForVLM(data, "doc.emf")
	if err != nil || !skip {
		t.Fatalf("metafile should skip VLM prepare, skip=%v err=%v", skip, err)
	}
}

func buildTestEMF(text string) []byte {
	return buildTestEMFWithOptions(text, 0)
}

func buildTestEMFWithOptions(text string, options uint32) []byte {
	u16 := utf16.Encode([]rune(text))
	strBytes := make([]byte, len(u16)*2)
	for i, c := range u16 {
		binary.LittleEndian.PutUint16(strBytes[i*2:], c)
	}
	pad := (4 - len(strBytes)%4) % 4

	const emrTextOff = 36
	offString := emrTextOff + 24 // Reference(8)+Chars(4)+offString(4)+Options(4)+offDx(4)
	extSize := offString + len(strBytes) + pad
	headerSize := 88
	total := headerSize + extSize + 20 // + EMR_EOF

	buf := make([]byte, total)
	// EMR_HEADER
	binary.LittleEndian.PutUint32(buf[0:], 1)
	binary.LittleEndian.PutUint32(buf[4:], uint32(headerSize))
	binary.LittleEndian.PutUint32(buf[40:], emfSignature)
	binary.LittleEndian.PutUint32(buf[44:], 0x10000)
	binary.LittleEndian.PutUint32(buf[48:], uint32(total))
	binary.LittleEndian.PutUint32(buf[52:], 3) // nRecords

	off := headerSize
	binary.LittleEndian.PutUint32(buf[off:], emfRecordExtTextOutW)
	binary.LittleEndian.PutUint32(buf[off+4:], uint32(extSize))
	binary.LittleEndian.PutUint32(buf[off+24:], 1) // GM_COMPATIBLE
	binary.LittleEndian.PutUint32(buf[off+emrTextOff+8:], uint32(len(u16)))
	binary.LittleEndian.PutUint32(buf[off+emrTextOff+12:], uint32(offString))
	binary.LittleEndian.PutUint32(buf[off+emrTextOff+16:], options)
	copy(buf[off+offString:], strBytes)

	off += extSize
	binary.LittleEndian.PutUint32(buf[off:], 14) // EMR_EOF
	binary.LittleEndian.PutUint32(buf[off+4:], 20)
	return buf
}
