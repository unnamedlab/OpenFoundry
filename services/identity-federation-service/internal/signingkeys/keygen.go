package signingkeys

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
)

// RSAKeyBits is the modulus size for every key this package mints.
// 2048 matches the ASVS-L2 floor for JWS signing keys.
const RSAKeyBits = 2048

// GenerateRSAKey mints a fresh RSA-2048 keypair.
func GenerateRSAKey() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, RSAKeyBits)
}

// EncodePrivateKeyPEM serialises a key to PKCS#8 PEM. PKCS#8 keeps
// the format algorithm-agnostic in case the package learns EdDSA
// later.
func EncodePrivateKeyPEM(priv *rsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("marshal pkcs8: %w", err)
	}
	block := &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	return pem.EncodeToMemory(block), nil
}

// EncodePublicKeyPEM serialises the public half to SubjectPublicKeyInfo
// PEM — the same shape Go's stdlib accepts on ParsePKIXPublicKey.
func EncodePublicKeyPEM(pub *rsa.PublicKey) ([]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("marshal pkix: %w", err)
	}
	block := &pem.Block{Type: "PUBLIC KEY", Bytes: der}
	return pem.EncodeToMemory(block), nil
}

// DecodePrivateKeyPEM parses PKCS#8 (or PKCS#1) PEM bytes back into a
// *rsa.PrivateKey.
func DecodePrivateKeyPEM(p []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(p)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("private key is not RSA")
		}
		return rsaKey, nil
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	return key, nil
}

// DecodePublicKeyPEM parses PKIX PEM bytes back into a *rsa.PublicKey.
func DecodePublicKeyPEM(p []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(p)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	pubAny, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	pub, ok := pubAny.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key is not RSA")
	}
	return pub, nil
}

// ComputeKid derives a stable kid from the SubjectPublicKeyInfo DER
// of the public key (RFC 7638-style — hash the canonical SPKI and
// base64url it). Two keys with the same modulus + exponent always
// produce the same kid; rotation always produces a fresh one.
func ComputeKid(pub *rsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", fmt.Errorf("marshal pkix: %w", err)
	}
	sum := sha256.Sum256(der)
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// PublicKeyToJwk projects an RSA public key into the JWK shape served
// by the publisher.
func PublicKeyToJwk(pub *rsa.PublicKey, kid string) Jwk {
	return Jwk{
		Kty: "RSA",
		Use: "sig",
		Alg: AlgorithmRS256,
		Kid: kid,
		N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(bigIntBytes(big.NewInt(int64(pub.E)))),
	}
}

// bigIntBytes returns the big-endian byte representation of v with
// leading zero bytes stripped, matching RFC 7518 § 6.3.1.1.
func bigIntBytes(v *big.Int) []byte {
	b := v.Bytes()
	for len(b) > 1 && b[0] == 0 {
		b = b[1:]
	}
	return b
}
