// Package basev100peer is a frozen test peer from Android v1.0.0.
// Only tests import it; it is not linked into the APK. See PROVENANCE.json.
package basev100peer

import "openflux/transport"

type Transport = transport.Transport

// Wrap reproduces the exact CLI/mobile construction order at the base commit.
func Wrap(raw Transport, codec, secret, context string, exit bool) (Transport, error) {
	var tr Transport = raw
	if codec == "legacy" {
		tr = NewCompressedTransport(tr)
	} else {
		tr = NewBatchedTransport(tr)
	}
	if secret != "" {
		return NewEncryptedTransport(tr, secret, context, exit)
	}
	return tr, nil
}

// DecodeCodec inspects a wire frame independently of current transport code.
func DecodeCodec(frame []byte, codec string) ([][]byte, error) {
	if codec == "legacy" {
		p, err := decompress(frame)
		return [][]byte{p}, err
	}
	return decodeBatch(frame)
}
