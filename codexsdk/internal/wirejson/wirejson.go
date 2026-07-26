package wirejson

import (
	"errors"
	"sync"
)

// Role identifies the protocol position admitting a JSON value.
type Role uint8

const (
	ActionBearingMessage Role = iota + 1
	ServerObservation
)

type ProtocolDecoder func(data []byte, target any, role Role) error

var (
	decoderMu sync.RWMutex
	decoder   ProtocolDecoder
)

// RegisterProtocolDecoder installs the generated protocol decoder during
// protocolv2 package initialization.
func RegisterProtocolDecoder(value ProtocolDecoder) {
	decoderMu.Lock()
	defer decoderMu.Unlock()
	if decoder != nil {
		panic("wirejson: protocol decoder already registered")
	}
	decoder = value
}

// Unmarshal admits JSON using the role selected by the transport boundary.
func Unmarshal(data []byte, target any, role Role) error {
	decoderMu.RLock()
	decode := decoder
	decoderMu.RUnlock()
	if decode == nil {
		return errors.New("wirejson: protocol decoder is not registered")
	}
	return decode(data, target, role)
}
