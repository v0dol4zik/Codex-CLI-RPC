package discord

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	payload := map[string]any{"evt": "READY", "text": "Привет"}
	frame, err := EncodeFrame(OpFrame, payload)
	if err != nil {
		t.Fatal(err)
	}
	opcode, decoded, err := DecodeFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	if opcode != OpFrame || decoded["evt"] != "READY" || decoded["text"] != "Привет" {
		t.Fatalf("unexpected frame: %d %+v", opcode, decoded)
	}
}

func TestFrameRejectsSizeMismatch(t *testing.T) {
	frame, err := EncodeFrame(OpFrame, map[string]any{"ok": true})
	if err != nil {
		t.Fatal(err)
	}
	frame = append(frame, 'x')
	if _, _, err := DecodeFrame(frame); err == nil {
		t.Fatal("expected size mismatch")
	}
}

func TestReadFrameRejectsOversizedPayloadBeforeAllocation(t *testing.T) {
	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header[0:4], OpFrame)
	binary.LittleEndian.PutUint32(header[4:8], maxFrameSize+1)
	if _, _, err := ReadFrame(bytes.NewReader(header)); err == nil {
		t.Fatal("expected oversized frame error")
	}
}

func TestDecodeFrameRejectsJSONNull(t *testing.T) {
	frame := make([]byte, 8+len("null"))
	binary.LittleEndian.PutUint32(frame[0:4], OpFrame)
	binary.LittleEndian.PutUint32(frame[4:8], 4)
	copy(frame[8:], "null")
	if _, _, err := DecodeFrame(frame); err == nil {
		t.Fatal("expected JSON object error")
	}
}
