// Package cipherclient documents the generated Functions cipher namespace.
// Runtime calls must forward the original caller token so cipher-service
// policy and audit evaluate as the function caller, not as the runtime host.
package cipherclient

// TypeScriptNamespace is the generated TS OSDK namespace shape.
const TypeScriptNamespace = `export const cipher = {
  encrypt: (keyId: string, value: unknown) => callCipher("encrypt", { key_id: keyId, plaintext: String(value) }),
  decrypt: (envelope: string) => callCipher("decrypt", { ciphertext: envelope }),
  tokenize: (pepperId: string, value: unknown) => callCipher("tokenize", { pepper_id: pepperId, plaintext: String(value) }),
};`

// PythonNamespace is the generated Python OSDK namespace shape.
const PythonNamespace = `class cipher:
    @staticmethod
    def encrypt(key_id, value): return call_cipher("encrypt", {"key_id": key_id, "plaintext": str(value)})
    @staticmethod
    def decrypt(envelope): return call_cipher("decrypt", {"ciphertext": envelope})
    @staticmethod
    def tokenize(pepper_id, value): return call_cipher("tokenize", {"pepper_id": pepper_id, "plaintext": str(value)})`

func RequiredCallerForwardingHeaders() []string {
	return []string{"Authorization", "X-OpenFoundry-Actor", "X-Request-Id"}
}
