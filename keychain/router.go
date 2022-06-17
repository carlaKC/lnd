package keychain

import "github.com/btcsuite/btcd/btcec/v2"

// RouterKeychain is an interface
type RouterKeychain interface {
	// Embed the SingleKeyECDH interface to include its functionality.
	SingleKeyECDH

	// Mul performs scalar multiplication with our node privkey. This
	// operation is used for route blinding.
	Mul(val *btcec.ModNScalar) *btcec.ModNScalar
}

// SingleKeyRouter is an implementation of the RouterKeychain interface. It
// holds an ECDH keyring and private key to be able to perform scalar
// multiplication for blinded routes.
type SingleKeyRouter struct {
	SingleKeyECDH

	NodePrivKey func() (*btcec.PrivateKey, error)
}

// Mul derives our node's private key and performs scalar multiplication with
// the value provided.
func (s *SingleKeyRouter) Mul(val *btcec.ModNScalar) *btcec.ModNScalar {
	privKey, _ := s.NodePrivKey()

	return privKey.Key.Mul(val)
}

// NewRouterKeychain creates a single key router instance for the key provided.
func NewRouterKeychain(keyDesc KeyDescriptor,
	keyRing SecretKeyRing) *SingleKeyRouter {

	return &SingleKeyRouter{
		SingleKeyECDH: NewPubKeyECDH(keyDesc, keyRing),
		NodePrivKey: func() (*btcec.PrivateKey, error) {
			return keyRing.DerivePrivKey(keyDesc)

		},
	}
}
