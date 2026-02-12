package secrets

// SecretStore provides encryption and decryption of sensitive values.
type SecretStore interface {
	Encrypt(plaintext string) (string, error)
	Decrypt(ciphertext string) (string, error)
}
