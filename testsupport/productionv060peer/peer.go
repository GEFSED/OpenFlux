// Package productionv060peer is a test-only peer frozen at production v0.6.0
// commit 081d214300c1067f17f6c0d02f84a8491f1a7b98. It is not used by the APK.
package productionv060peer

import "openflux/transport"

type Transport = transport.Transport

// WrapLegacy reproduces main.go/mobile.go at 081d214: raw -> AES -> Legacy.
func WrapLegacy(raw Transport, secret, context string, exit bool) (Transport, error) {
	return WrapLegacyObserved(raw, secret, context, exit, nil)
}

// WrapLegacyObserved adds a transparent test tap after successful AES receive.
// The copied AES/Legacy implementations, including fallback, remain unchanged.
func WrapLegacyObserved(raw Transport, secret, context string, exit bool, observe func([]byte)) (Transport, error) {
	var inner Transport = raw
	if secret != "" {
		encrypted, err := NewEncryptedTransport(inner, secret, context, exit)
		if err != nil {
			return nil, err
		}
		inner = encrypted
	}
	if observe != nil {
		inner = &receiveTap{Transport: inner, observe: observe}
	}
	return NewCompressedTransport(inner), nil
}

type receiveTap struct {
	Transport
	observe func([]byte)
}

func (r *receiveTap) Receive(callback func([]byte)) {
	r.Transport.Receive(func(data []byte) {
		r.observe(data)
		callback(data)
	})
}

// DecodeLegacy bypasses the permissive callback fallback for test assertions.
func DecodeLegacy(data []byte) ([]byte, error) { return decompress(data) }
