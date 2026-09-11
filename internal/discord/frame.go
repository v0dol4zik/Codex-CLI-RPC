package discord

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

const (
	OpHandshake uint32 = iota
	OpFrame
	OpClose
	OpPing
	OpPong
	maxFrameSize = 8 * 1024 * 1024
)

type Error struct {
	Message string
}

func (err *Error) Error() string { return err.Message }

func EncodeFrame(opcode uint32, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode Discord payload: %w", err)
	}
	if len(body) > maxFrameSize {
		return nil, &Error{Message: "Discord IPC frame is too large"}
	}
	frame := make([]byte, 8+len(body))
	binary.LittleEndian.PutUint32(frame[0:4], opcode)
	binary.LittleEndian.PutUint32(frame[4:8], uint32(len(body)))
	copy(frame[8:], body)
	return frame, nil
}

func DecodeFrame(frame []byte) (uint32, map[string]any, error) {
	if len(frame) < 8 {
		return 0, nil, &Error{Message: "Discord IPC frame is shorter than its header"}
	}
	size := binary.LittleEndian.Uint32(frame[4:8])
	if size > maxFrameSize {
		return 0, nil, &Error{Message: "Discord IPC frame is too large"}
	}
	if int(size) != len(frame)-8 {
		return 0, nil, &Error{Message: fmt.Sprintf("Discord IPC frame size mismatch: expected %d, got %d", size, len(frame)-8)}
	}
	payload := make(map[string]any)
	if err := json.Unmarshal(frame[8:], &payload); err != nil {
		return 0, nil, &Error{Message: fmt.Sprintf("Discord IPC payload is invalid: %v", err)}
	}
	if payload == nil {
		return 0, nil, &Error{Message: "Discord IPC payload must be a JSON object"}
	}
	return binary.LittleEndian.Uint32(frame[0:4]), payload, nil
}

func ReadFrame(reader io.Reader) (uint32, map[string]any, error) {
	header := make([]byte, 8)
	if _, err := io.ReadFull(reader, header); err != nil {
		return 0, nil, &Error{Message: fmt.Sprintf("read Discord IPC header: %v", err)}
	}
	size := binary.LittleEndian.Uint32(header[4:8])
	if size > maxFrameSize {
		return 0, nil, &Error{Message: "Discord IPC frame is too large"}
	}
	frame := make([]byte, 8+int(size))
	copy(frame, header)
	if _, err := io.ReadFull(reader, frame[8:]); err != nil {
		return 0, nil, &Error{Message: fmt.Sprintf("read Discord IPC payload: %v", err)}
	}
	return DecodeFrame(frame)
}
