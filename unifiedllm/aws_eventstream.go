package unifiedllm

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
)

var crc32cTable = crc32.MakeTable(crc32.Castagnoli)

// eventStreamMessage represents a decoded AWS event stream message.
type eventStreamMessage struct {
	Headers map[string]string
	Payload []byte
}

// eventStreamReader reads AWS event stream binary format.
type eventStreamReader struct {
	reader io.Reader
}

func newEventStreamReader(r io.Reader) *eventStreamReader {
	return &eventStreamReader{reader: r}
}

// Next reads and returns the next event stream message.
// Returns io.EOF when the stream ends.
func (r *eventStreamReader) Next() (*eventStreamMessage, error) {
	// Read the prelude: total length (4) + headers length (4)
	prelude := make([]byte, 8)
	if _, err := io.ReadFull(r.reader, prelude); err != nil {
		return nil, err
	}

	totalLength := binary.BigEndian.Uint32(prelude[0:4])
	headersLength := binary.BigEndian.Uint32(prelude[4:8])

	// Sanity check
	if totalLength < 16 { // minimum: 8 (prelude) + 4 (prelude CRC) + 0 + 4 (message CRC)
		return nil, fmt.Errorf("event stream message too short: %d bytes", totalLength)
	}

	// Read prelude CRC (4 bytes)
	preludeCRCBytes := make([]byte, 4)
	if _, err := io.ReadFull(r.reader, preludeCRCBytes); err != nil {
		return nil, err
	}

	expectedPreludeCRC := crc32.Checksum(prelude, crc32cTable)
	actualPreludeCRC := binary.BigEndian.Uint32(preludeCRCBytes)
	if expectedPreludeCRC != actualPreludeCRC {
		return nil, fmt.Errorf("prelude CRC mismatch: expected %08x, got %08x", expectedPreludeCRC, actualPreludeCRC)
	}

	// Read headers
	headersBytes := make([]byte, headersLength)
	if headersLength > 0 {
		if _, err := io.ReadFull(r.reader, headersBytes); err != nil {
			return nil, err
		}
	}

	// Calculate payload length:
	// total = 4 (total len) + 4 (headers len) + 4 (prelude CRC) + headers + payload + 4 (message CRC)
	payloadLength := totalLength - 12 - headersLength - 4

	// Read payload
	payload := make([]byte, payloadLength)
	if payloadLength > 0 {
		if _, err := io.ReadFull(r.reader, payload); err != nil {
			return nil, err
		}
	}

	// Read message CRC (4 bytes)
	messageCRCBytes := make([]byte, 4)
	if _, err := io.ReadFull(r.reader, messageCRCBytes); err != nil {
		return nil, err
	}

	// Verify message CRC over everything except the message CRC itself
	messageData := make([]byte, 0, totalLength-4)
	messageData = append(messageData, prelude...)
	messageData = append(messageData, preludeCRCBytes...)
	messageData = append(messageData, headersBytes...)
	messageData = append(messageData, payload...)

	expectedMessageCRC := crc32.Checksum(messageData, crc32cTable)
	actualMessageCRC := binary.BigEndian.Uint32(messageCRCBytes)
	if expectedMessageCRC != actualMessageCRC {
		return nil, fmt.Errorf("message CRC mismatch: expected %08x, got %08x", expectedMessageCRC, actualMessageCRC)
	}

	headers := parseEventStreamHeaders(headersBytes)

	return &eventStreamMessage{
		Headers: headers,
		Payload: payload,
	}, nil
}

// parseEventStreamHeaders decodes AWS event stream headers.
// Header format: [1-byte name length][name][1-byte value type][type-specific value]
func parseEventStreamHeaders(data []byte) map[string]string {
	headers := make(map[string]string)
	offset := 0

	for offset < len(data) {
		// Header name length (1 byte)
		if offset >= len(data) {
			break
		}
		nameLen := int(data[offset])
		offset++

		if offset+nameLen > len(data) {
			break
		}
		name := string(data[offset : offset+nameLen])
		offset += nameLen

		// Value type (1 byte)
		if offset >= len(data) {
			break
		}
		valueType := data[offset]
		offset++

		switch valueType {
		case 0: // bool true
			headers[name] = "true"
		case 1: // bool false
			headers[name] = "false"
		case 2: // byte
			if offset+1 > len(data) {
				return headers
			}
			offset++
		case 3: // short
			if offset+2 > len(data) {
				return headers
			}
			offset += 2
		case 4: // int
			if offset+4 > len(data) {
				return headers
			}
			offset += 4
		case 5: // long
			if offset+8 > len(data) {
				return headers
			}
			offset += 8
		case 6: // bytes
			if offset+2 > len(data) {
				return headers
			}
			valueLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
			offset += 2
			if offset+valueLen > len(data) {
				return headers
			}
			offset += valueLen
		case 7: // string
			if offset+2 > len(data) {
				return headers
			}
			valueLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
			offset += 2
			if offset+valueLen > len(data) {
				return headers
			}
			headers[name] = string(data[offset : offset+valueLen])
			offset += valueLen
		case 8: // timestamp
			if offset+8 > len(data) {
				return headers
			}
			offset += 8
		case 9: // uuid
			if offset+16 > len(data) {
				return headers
			}
			offset += 16
		default:
			return headers
		}
	}

	return headers
}

// buildEventStreamMessage constructs a binary event stream message.
// Used internally for testing.
func buildEventStreamMessage(headers map[string]string, payload []byte) []byte {
	// Encode headers
	var headersBuf []byte
	for name, value := range headers {
		headersBuf = append(headersBuf, byte(len(name)))
		headersBuf = append(headersBuf, []byte(name)...)
		headersBuf = append(headersBuf, 7) // string type
		valBytes := []byte(value)
		headersBuf = append(headersBuf, byte(len(valBytes)>>8), byte(len(valBytes)))
		headersBuf = append(headersBuf, valBytes...)
	}

	headersLength := uint32(len(headersBuf))
	totalLength := 4 + 4 + 4 + headersLength + uint32(len(payload)) + 4

	// Build prelude
	prelude := make([]byte, 8)
	binary.BigEndian.PutUint32(prelude[0:4], totalLength)
	binary.BigEndian.PutUint32(prelude[4:8], headersLength)

	// Prelude CRC
	preludeCRC := crc32.Checksum(prelude, crc32cTable)
	preludeCRCBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(preludeCRCBytes, preludeCRC)

	// Assemble message (minus message CRC)
	var msg []byte
	msg = append(msg, prelude...)
	msg = append(msg, preludeCRCBytes...)
	msg = append(msg, headersBuf...)
	msg = append(msg, payload...)

	// Message CRC
	messageCRC := crc32.Checksum(msg, crc32cTable)
	messageCRCBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(messageCRCBytes, messageCRC)
	msg = append(msg, messageCRCBytes...)

	return msg
}
